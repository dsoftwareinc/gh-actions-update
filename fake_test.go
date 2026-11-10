package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const sampleWorkflow = `name: CI
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3.5.0
      - uses: actions/setup-python@v4.0.0
      - name: Run tests
        run: pytest
`

var now = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func daysAgo(days int) time.Time { return now.AddDate(0, 0, -days) }

// fakeAPI is an in-memory githubAPI. Missing releases, commits and files are reported as errNotFound.
type fakeAPI struct {
	mu       sync.Mutex
	releases map[string]release           // repo -> latest release
	commits  map[string]commit            // repo@ref -> commit
	files    map[string]map[string]string // repo -> path -> content
	err      error                        // returned by every call when set
	calls    map[string]int               // method name -> number of calls
	puts     []filePut
}

type filePut struct {
	repo, path, message, content, sha string
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		releases: map[string]release{},
		commits:  map[string]commit{},
		files:    map[string]map[string]string{},
		calls:    map[string]int{},
	}
}

func (f *fakeAPI) addRelease(repo, tag string, published time.Time) {
	f.releases[repo] = release{Tag: tag, Published: published}
}

func (f *fakeAPI) addCommit(repo, ref, sha string, date time.Time) {
	f.commits[repo+"@"+ref] = commit{SHA: sha, Date: date}
}

func (f *fakeAPI) addFile(repo, path, content string) {
	if f.files[repo] == nil {
		f.files[repo] = map[string]string{}
	}
	f.files[repo][path] = content
}

func (f *fakeAPI) record(method string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[method]++
	return f.err
}

func (f *fakeAPI) callCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[method]
}

func (f *fakeAPI) CheckRepo(repo string) error {
	if err := f.record("CheckRepo"); err != nil {
		return err
	}
	if _, ok := f.files[repo]; !ok {
		return fmt.Errorf("repo %s: %w", repo, errNotFound)
	}
	return nil
}

func (f *fakeAPI) LatestRelease(repo string) (release, error) {
	if err := f.record("LatestRelease"); err != nil {
		return release{}, err
	}
	rel, ok := f.releases[repo]
	if !ok {
		return release{}, fmt.Errorf("release of %s: %w", repo, errNotFound)
	}
	return rel, nil
}

func (f *fakeAPI) Commit(repo, ref string) (commit, error) {
	if err := f.record("Commit"); err != nil {
		return commit{}, err
	}
	c, ok := f.commits[repo+"@"+ref]
	if !ok {
		return commit{}, fmt.Errorf("commit %s of %s: %w", ref, repo, errNotFound)
	}
	return c, nil
}

func (f *fakeAPI) ListDir(repo, dir string) ([]string, error) {
	if err := f.record("ListDir"); err != nil {
		return nil, err
	}
	files, ok := f.files[repo]
	if !ok {
		return nil, fmt.Errorf("repo %s: %w", repo, errNotFound)
	}
	var paths []string
	for p := range files {
		if filepath.ToSlash(filepath.Dir(p)) == dir {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

func (f *fakeAPI) GetFile(repo, path string) ([]byte, string, error) {
	if err := f.record("GetFile"); err != nil {
		return nil, "", err
	}
	content, ok := f.files[repo][path]
	if !ok {
		return nil, "", fmt.Errorf("file %s of %s: %w", path, repo, errNotFound)
	}
	return []byte(content), "sha-of-" + path, nil
}

func (f *fakeAPI) PutFile(repo, path, message string, content []byte, sha string) error {
	if err := f.record("PutFile"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.puts = append(f.puts, filePut{repo, path, message, string(content), sha})
	f.files[repo][path] = string(content)
	return nil
}

// newLocalRepo creates a git repository with .github/workflows/ci.yml containing sampleWorkflow.
func newLocalRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	writeFile(t, filepath.Join(root, ".github", "workflows", "ci.yml"), sampleWorkflow)
	return root
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
