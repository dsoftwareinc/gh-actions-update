package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// maxConcurrentLookups bounds parallel GitHub API requests while resolving latest versions.
const maxConcurrentLookups = 8

// actionVersion is one `uses: name@current` reference; Latest is empty when no newer version was found.
type actionVersion struct {
	Name    string
	Current string
	Latest  string
}

// updater finds the latest release of actions. It is safe for concurrent use, and every release and commit is
// fetched at most once.
type updater struct {
	api       githubAPI
	majorOnly bool
	log       *logger

	releases memo[*release] // OWNER/REPO -> latest release, nil when the repo has none
	commits  memo[*commit]  // OWNER/REPO@ref -> commit, nil when it doesn't exist
}

func newUpdater(api githubAPI, majorOnly bool, log *logger) *updater {
	return &updater{api: api, majorOnly: majorOnly, log: log}
}

// latestVersions resolves the latest version of each `uses` reference concurrently. The result maps every
// reference to its newer version, or to "" when it is up to date or can't be checked.
func (u *updater) latestVersions(refs []string) (map[string]string, error) {
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		errs    []error
		results = make(map[string]string, len(refs))
		sem     = make(chan struct{}, maxConcurrentLookups)
	)
	for _, ref := range refs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			latest, err := u.latestVersion(ref)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", ref, err))
				return
			}
			results[ref] = latest
		})
	}
	wg.Wait()
	return results, errors.Join(errs...)
}

// latestVersion returns the latest release tag of the action in a `uses` value (e.g. actions/checkout@v4) when it
// is newer than the referenced version, and "" otherwise. See
// https://docs.github.com/en/actions/writing-workflows/workflow-syntax-for-github-actions#jobsjob_idstepsuses
func (u *updater) latestVersion(uses string) (string, error) {
	name, current, ok := strings.Cut(uses, "@")
	if !ok || current == "" {
		return "", nil
	}
	repo, ok := ownerRepo(name)
	if !ok {
		return "", nil
	}
	rel, err := u.latestRelease(repo)
	if err != nil || rel == nil {
		return "", err
	}
	if isSHA(current) {
		newer, err := u.isReleaseNewerThanCommit(repo, rel.Tag, current)
		if err != nil || !newer {
			return "", err
		}
		return fixVersion(rel.Tag, u.majorOnly), nil
	}
	cmp, err := compareVersions(rel.Tag, current, u.majorOnly)
	if err != nil {
		u.log.Infof("Could not compare versions %s and %s of %s: %v", rel.Tag, current, name, err)
		return "", nil
	}
	if cmp > 0 {
		return fixVersion(rel.Tag, u.majorOnly), nil
	}
	return "", nil
}

func (u *updater) latestRelease(repo string) (*release, error) {
	return u.releases.get(repo, func() (*release, error) {
		u.log.Infof("Getting latest release for repository: %s", repo)
		rel, err := u.api.LatestRelease(repo)
		if errors.Is(err, errNotFound) {
			u.log.Warnf("No releases found for repository: %s", repo)
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &rel, nil
	})
}

func (u *updater) commit(repo, ref string) (*commit, error) {
	return u.commits.get(repo+"@"+ref, func() (*commit, error) {
		u.log.Debugf("Getting commit %s of %s", ref, repo)
		c, err := u.api.Commit(repo, ref)
		if errors.Is(err, errNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &c, nil
	})
}

// isReleaseNewerThanCommit reports whether the commit of release tag is newer than the commit an action is
// pinned to. A pin to the release's own commit is up to date.
func (u *updater) isReleaseNewerThanCommit(repo, tag, sha string) (bool, error) {
	u.log.Debugf("%s is pinned to SHA %s, checking whether %s is newer", repo, sha, tag)
	tagCommit, err := u.commit(repo, tag)
	if err != nil {
		return false, err
	}
	if tagCommit == nil {
		u.log.Warnf("Commit of release %s not found in repository: %s", tag, repo)
		return false, nil
	}
	if strings.EqualFold(tagCommit.SHA, sha) {
		return false, nil
	}
	pinned, err := u.commit(repo, sha)
	if err != nil {
		return false, err
	}
	if pinned == nil {
		u.log.Warnf("Commit %s not found in repository: %s", sha, repo)
		return false, nil
	}
	return tagCommit.Date.After(pinned.Date), nil
}

// ownerRepo returns the OWNER/REPO an action lives in: actions like owner/repo/sub/path are in owner/repo.
func ownerRepo(action string) (string, bool) {
	parts := strings.Split(action, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return parts[0] + "/" + parts[1], true
}

// isSHA reports whether version is a full 40-character commit SHA.
func isSHA(version string) bool {
	if len(version) != 40 {
		return false
	}
	for _, c := range strings.ToLower(version) {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// fixVersion trims a tag to its major version (v1.2.3 -> v1) when majorOnly is set.
func fixVersion(tag string, majorOnly bool) string {
	if majorOnly {
		major, _, _ := strings.Cut(tag, ".")
		return major
	}
	return tag
}

// compareVersions compares dotted numeric versions with an optional "v" prefix, returning 1 if v1 > v2, 0 if
// they are equal and -1 if v1 < v2. Missing segments count as 0. With majorOnly only the major versions are
// compared. It fails when it reaches a segment that is not a number, e.g. in "main" or "release/v1".
func compareVersions(v1, v2 string, majorOnly bool) (int, error) {
	s1 := strings.Split(strings.TrimPrefix(v1, "v"), ".")
	s2 := strings.Split(strings.TrimPrefix(v2, "v"), ".")
	if majorOnly {
		s1, s2 = s1[:1], s2[:1]
	}
	for i := range max(len(s1), len(s2)) {
		n1, err := versionSegment(s1, i)
		if err != nil {
			return 0, err
		}
		n2, err := versionSegment(s2, i)
		if err != nil {
			return 0, err
		}
		if n1 != n2 {
			if n1 > n2 {
				return 1, nil
			}
			return -1, nil
		}
	}
	return 0, nil
}

func versionSegment(segments []string, i int) (int, error) {
	if i >= len(segments) {
		return 0, nil
	}
	return strconv.Atoi(segments[i])
}

// memo caches the result of loading each key, loading it once even when requested concurrently.
type memo[T any] struct {
	mu      sync.Mutex
	entries map[string]*memoEntry[T]
}

type memoEntry[T any] struct {
	once sync.Once
	val  T
	err  error
}

func (m *memo[T]) get(key string, load func() (T, error)) (T, error) {
	m.mu.Lock()
	if m.entries == nil {
		m.entries = make(map[string]*memoEntry[T])
	}
	e, ok := m.entries[key]
	if !ok {
		e = &memoEntry[T]{}
		m.entries[key] = e
	}
	m.mu.Unlock()
	e.once.Do(func() { e.val, e.err = load() })
	return e.val, e.err
}
