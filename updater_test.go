package main

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestIsSHA(t *testing.T) {
	tests := map[string]bool{
		strings.Repeat("a", 40):                            true,
		strings.Repeat("0123456789abcdef", 2) + "01234567": true,
		strings.Repeat("ABCDEF1234567890", 2) + "ABCDEF12": true,
		"v1.2.3":                false,
		"deadbeef":              false,
		strings.Repeat("a", 39): false,
		strings.Repeat("a", 41): false,
		strings.Repeat("z", 40): false,
	}
	for version, want := range tests {
		if got := isSHA(version); got != want {
			t.Errorf("isSHA(%q) = %v, want %v", version, got, want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1, v2    string
		majorOnly bool
		want      int
	}{
		{"v1.2.4", "v1.2.3", false, 1},
		{"v1.2.3", "v1.2.4", false, -1},
		{"v1.2.3", "v1.2.3", false, 0},
		{"v2.0.0", "v1.9.9", false, 1},
		{"v1.3.0", "v1.2.9", false, 1},
		{"1.2.4", "1.2.3", false, 1},
		{"1.2", "1.2.1", false, -1},
		{"1.2.1", "1.2", false, 1},
		{"v7.0.1", "v7", false, 1},
		{"v1.9.9", "v1.0.0", true, 0},
		{"v2.0.0", "v1.9.9", true, 1},
		{"v7.0.1", "v7", true, 0},
		{"v2", "v1.x", false, 1}, // decided before reaching the unparsable segment
	}
	for _, tt := range tests {
		got, err := compareVersions(tt.v1, tt.v2, tt.majorOnly)
		if err != nil || got != tt.want {
			t.Errorf("compareVersions(%q, %q, %v) = %d, %v; want %d", tt.v1, tt.v2, tt.majorOnly, got, err, tt.want)
		}
	}
}

func TestCompareVersionsNotANumber(t *testing.T) {
	for _, v := range []string{"main", "release/v1", "v1.x"} {
		if _, err := compareVersions("v1.14.3", v, false); err == nil {
			t.Errorf("compareVersions(v1.14.3, %q) did not fail", v)
		}
	}
}

func TestFixVersion(t *testing.T) {
	tests := []struct {
		tag       string
		majorOnly bool
		want      string
	}{
		{"v1.2.3", false, "v1.2.3"},
		{"v1.2.3", true, "v1"},
		{"v4", true, "v4"},
	}
	for _, tt := range tests {
		if got := fixVersion(tt.tag, tt.majorOnly); got != tt.want {
			t.Errorf("fixVersion(%q, %v) = %q, want %q", tt.tag, tt.majorOnly, got, tt.want)
		}
	}
}

func TestOwnerRepo(t *testing.T) {
	tests := map[string]string{
		"actions/checkout":              "actions/checkout",
		"actions/upload-artifact/merge": "actions/upload-artifact",
		"github/codeql-action/init":     "github/codeql-action",
		"checkout":                      "",
		"/checkout":                     "",
	}
	for action, want := range tests {
		got, ok := ownerRepo(action)
		if got != want || ok != (want != "") {
			t.Errorf("ownerRepo(%q) = %q, %v; want %q", action, got, ok, want)
		}
	}
}

func testUpdater(api githubAPI, majorOnly bool) *updater {
	return newUpdater(api, majorOnly, &logger{w: io.Discard})
}

func assertLatest(t *testing.T, u *updater, uses, want string) {
	t.Helper()
	got, err := u.latestVersion(uses)
	if err != nil {
		t.Fatalf("latestVersion(%q) failed: %v", uses, err)
	}
	if got != want {
		t.Errorf("latestVersion(%q) = %q, want %q", uses, got, want)
	}
}

func TestLatestVersionSemver(t *testing.T) {
	tests := []struct {
		name, uses, release, want string
	}{
		{"newer release", "actions/checkout@v2.0.0", "v3.0.0", "v3.0.0"},
		{"same version", "actions/checkout@v2.0.0", "v2.0.0", ""},
		{"older release", "actions/checkout@v2.0.0", "v1.0.0", ""},
		{"major tag behind patch release", "actions/checkout@v7", "v7.0.1", "v7.0.1"},
		{"branch ref", "pypa/gh-action-pypi-publish@release/v1", "v1.14.3", ""},
		{"no ref", "actions/checkout", "v3.0.0", ""},
		{"empty ref", "actions/checkout@", "v3.0.0", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeAPI()
			repo, _ := ownerRepo(strings.SplitN(tt.uses, "@", 2)[0])
			api.addRelease(repo, tt.release, now)
			assertLatest(t, testUpdater(api, false), tt.uses, tt.want)
		})
	}
}

func TestLatestVersionNoReleases(t *testing.T) {
	// The fake has no release for the repository, as for a repository without releases or that doesn't exist.
	assertLatest(t, testUpdater(newFakeAPI(), false), "nonexistent/action@v1.0.0", "")
}

func TestLatestVersionAPIError(t *testing.T) {
	api := newFakeAPI()
	api.err = errors.New("rate limited")
	if _, err := testUpdater(api, false).latestVersion("actions/checkout@v1"); err == nil {
		t.Fatal("expected the API error to be returned")
	}
}

func TestLatestVersionSubPathQueriesOwnerRepo(t *testing.T) {
	api := newFakeAPI()
	api.addRelease("actions/upload-artifact", "v4.0.0", now)
	u := testUpdater(api, false)
	assertLatest(t, u, "actions/upload-artifact/merge@v3.0.0", "v4.0.0")
	assertLatest(t, u, "actions/upload-artifact@v3.0.0", "v4.0.0")
	if n := api.callCount("LatestRelease"); n != 1 {
		t.Errorf("LatestRelease called %d times, want 1 (cached per repository)", n)
	}
}

func TestLatestVersionCachesReleases(t *testing.T) {
	api := newFakeAPI()
	api.addRelease("actions/checkout", "v3.0.0", now)
	u := testUpdater(api, false)
	assertLatest(t, u, "actions/checkout@v2.0.0", "v3.0.0")
	assertLatest(t, u, "actions/checkout@v2.0.0", "v3.0.0")
	assertLatest(t, u, "actions/checkout@v3.0.0", "")
	if n := api.callCount("LatestRelease"); n != 1 {
		t.Errorf("LatestRelease called %d times, want 1", n)
	}
}

func TestLatestVersionMajorOnly(t *testing.T) {
	api := newFakeAPI()
	api.addRelease("actions/checkout", "v7.0.1", now)
	api.addRelease("actions/setup-go", "v4.1.0", now)
	u := testUpdater(api, true)
	assertLatest(t, u, "actions/setup-go@v3.0.0", "v4")
	// Every workflow using an action gets the same major tag.
	assertLatest(t, u, "actions/checkout@v5", "v7")
	assertLatest(t, u, "actions/checkout@v5", "v7")
	assertLatest(t, u, "actions/checkout@v7", "")
	assertLatest(t, u, "actions/checkout@v7.0.0", "")
}

const (
	shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	shaC = "cccccccccccccccccccccccccccccccccccccccc"
	shaR = "1111111111111111111111111111111111111111" // commit of the latest release
)

// shaPinnedAPI returns an API where actions/checkout's latest release v3.0.0 is a commit made releaseAgeDays
// ago, and each of the given SHAs is a commit of the given age.
func shaPinnedAPI(tag string, releaseAgeDays int, commitAgesDays map[string]int) *fakeAPI {
	api := newFakeAPI()
	api.addRelease("actions/checkout", tag, daysAgo(releaseAgeDays))
	api.addCommit("actions/checkout", tag, shaR, daysAgo(releaseAgeDays))
	for sha, age := range commitAgesDays {
		api.addCommit("actions/checkout", sha, sha, daysAgo(age))
	}
	return api
}

func TestLatestVersionSHAPinned(t *testing.T) {
	tests := []struct {
		name           string
		releaseAgeDays int
		pinnedAgeDays  int
		want           string
	}{
		{"release newer than pinned commit", 1, 30, "v3.0.0"},
		{"pinned commit newer than release", 7, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := shaPinnedAPI("v3.0.0", tt.releaseAgeDays, map[string]int{shaA: tt.pinnedAgeDays})
			assertLatest(t, testUpdater(api, false), "actions/checkout@"+shaA, tt.want)
		})
	}
}

func TestLatestVersionPinnedToReleaseCommit(t *testing.T) {
	api := shaPinnedAPI("v3.0.0", 1, nil)
	u := testUpdater(api, false)
	assertLatest(t, u, "actions/checkout@"+shaR, "")
	assertLatest(t, u, "actions/checkout@"+strings.ToUpper(shaR), "")
}

func TestLatestVersionSameSHATwice(t *testing.T) {
	api := shaPinnedAPI("v3.0.0", 1, map[string]int{shaA: 30})
	u := testUpdater(api, false)
	assertLatest(t, u, "actions/checkout@"+shaA, "v3.0.0")
	assertLatest(t, u, "actions/checkout@"+shaA, "v3.0.0")
	if n := api.callCount("Commit"); n != 2 {
		t.Errorf("Commit called %d times, want 2 (release tag and pinned SHA, each once)", n)
	}
}

func TestLatestVersionComparesEachSHAWithRelease(t *testing.T) {
	api := shaPinnedAPI("v3.0.0", 10, map[string]int{shaA: 30, shaB: 1})
	u := testUpdater(api, false)
	assertLatest(t, u, "actions/checkout@"+shaA, "v3.0.0")
	assertLatest(t, u, "actions/checkout@"+shaB, "") // newer than the release
}

func TestLatestVersionSHAAfterSemverOfSameAction(t *testing.T) {
	api := shaPinnedAPI("v3.0.0", 1, map[string]int{shaA: 30})
	u := testUpdater(api, false)
	assertLatest(t, u, "actions/checkout@v2.0.0", "v3.0.0")
	assertLatest(t, u, "actions/checkout@"+shaA, "v3.0.0")
}

func TestLatestVersionUnknownSHA(t *testing.T) {
	api := shaPinnedAPI("v3.0.0", 1, map[string]int{shaA: 30})
	u := testUpdater(api, false)
	assertLatest(t, u, "actions/checkout@"+shaC, "")
	assertLatest(t, u, "actions/checkout@"+shaA, "v3.0.0")
	assertLatest(t, u, "actions/checkout@"+shaC, "")
}

func TestLatestVersionSHAPinnedMajorOnly(t *testing.T) {
	api := shaPinnedAPI("v7.0.1", 0, map[string]int{shaA: 30})
	assertLatest(t, testUpdater(api, true), "actions/checkout@"+shaA, "v7")
}

func TestLatestVersionsConcurrent(t *testing.T) {
	api := shaPinnedAPI("v3.0.0", 1, map[string]int{shaA: 30, shaB: 0})
	api.addRelease("actions/setup-go", "v6.0.0", now)
	refs := []string{
		"actions/checkout@v2", "actions/checkout@v3.0.0", "actions/checkout@" + shaA, "actions/checkout@" + shaB,
		"actions/setup-go@v5", "actions/setup-go@v6.0.0", "no/releases@v1",
	}
	got, err := testUpdater(api, false).latestVersions(refs)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"actions/checkout@v2": "v3.0.0", "actions/checkout@v3.0.0": "", "actions/checkout@" + shaA: "v3.0.0",
		"actions/checkout@" + shaB: "", "actions/setup-go@v5": "v6.0.0", "actions/setup-go@v6.0.0": "",
		"no/releases@v1": "",
	}
	if len(got) != len(want) {
		t.Errorf("got %d results, want %d: %v", len(got), len(want), got)
	}
	for ref, w := range want {
		if got[ref] != w {
			t.Errorf("%s: got %q, want %q", ref, got[ref], w)
		}
	}
	if n := api.callCount("LatestRelease"); n != 3 {
		t.Errorf("LatestRelease called %d times, want 3 (once per repository)", n)
	}
}

func TestLatestVersionsReturnsErrors(t *testing.T) {
	api := newFakeAPI()
	api.err = errors.New("rate limited")
	_, err := testUpdater(api, false).latestVersions([]string{"actions/checkout@v1", "actions/setup-go@v1"})
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("got error %v, want rate limited", err)
	}
}
