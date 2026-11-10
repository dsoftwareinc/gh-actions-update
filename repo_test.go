package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func noAPI() (githubAPI, error) {
	return nil, errors.New("the API must not be used for local repositories")
}

func TestOpenRepoLocal(t *testing.T) {
	root := newLocalRepo(t)
	repo, err := openRepo(root, noAPI)
	if err != nil {
		t.Fatal(err)
	}
	if repo.IsRemote() || repo.String() != root {
		t.Errorf("got %v (remote: %v), want local %s", repo, repo.IsRemote(), root)
	}
}

func TestOpenRepoFromSubdirectoryFindsRoot(t *testing.T) {
	root := newLocalRepo(t)
	sub := filepath.Join(root, "docs", "guide")
	mustMkdir(t, sub)
	repo, err := openRepo(sub, noAPI)
	if err != nil {
		t.Fatal(err)
	}
	if repo.String() != root {
		t.Errorf("got %s, want %s", repo, root)
	}
}

func TestOpenRepoDirectoryOutsideGit(t *testing.T) {
	dir := t.TempDir()
	repo, err := openRepo(dir, noAPI)
	if err != nil {
		t.Fatal(err)
	}
	if repo.IsRemote() || repo.String() != dir {
		t.Errorf("got %v, want local %s", repo, dir)
	}
}

func TestOpenRepoRemote(t *testing.T) {
	api := newFakeAPI()
	api.addFile("owner/repo", "README.md", "")
	t.Chdir(t.TempDir())
	repo, err := openRepo("owner/repo", func() (githubAPI, error) { return api, nil })
	if err != nil {
		t.Fatal(err)
	}
	if !repo.IsRemote() || repo.String() != "owner/repo" {
		t.Errorf("got %v, want remote owner/repo", repo)
	}
}

func TestOpenRepoRemoteNotFound(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := openRepo("owner/missing", func() (githubAPI, error) { return newFakeAPI(), nil })
	if err == nil || !strings.Contains(err.Error(), "repository owner/missing not found") {
		t.Errorf("got %v", err)
	}
}

func TestOpenRepoInvalid(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, arg := range []string{"./local-but-missing", "../x", "owner/.", "-owner/repo", "/nonexistent/path/xyz", "owner", "a/b/c"} {
		if _, err := openRepo(arg, noAPI); err == nil {
			t.Errorf("openRepo(%q) did not fail", arg)
		}
	}
}

func TestLocalWorkflowPaths(t *testing.T) {
	root := newLocalRepo(t)
	dir := filepath.Join(root, ".github", "workflows")
	writeFile(t, filepath.Join(dir, "b.yaml"), "")
	writeFile(t, filepath.Join(dir, "notes.txt"), "")
	mustMkdir(t, filepath.Join(dir, "sub.yml"))
	paths, err := localRepo{root: root}.WorkflowPaths()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".github/workflows/b.yaml", ".github/workflows/ci.yml"}; !slices.Equal(paths, want) {
		t.Errorf("got %v, want %v", paths, want)
	}
}

func TestLocalWorkflowPathsWithoutWorkflowsDir(t *testing.T) {
	paths, err := localRepo{root: t.TempDir()}.WorkflowPaths()
	if err != nil || len(paths) != 0 {
		t.Errorf("got %v, %v; want no paths", paths, err)
	}
}

func TestLocalReadWriteWorkflow(t *testing.T) {
	root := newLocalRepo(t)
	repo := localRepo{root: root}
	content, err := repo.ReadWorkflow(".github/workflows/ci.yml")
	if err != nil || string(content) != sampleWorkflow {
		t.Fatalf("got %q, %v", content, err)
	}
	updated := "name: Ünïcödé CI\n"
	if err := repo.WriteWorkflow(".github/workflows/ci.yml", []byte(updated), "ignored"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(root, ".github", "workflows", "ci.yml")); got != updated {
		t.Errorf("got %q, want %q", got, updated)
	}
	if _, err := repo.ReadWorkflow(".github/workflows/missing.yml"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("reading a missing workflow returned %v, want fs.ErrNotExist", err)
	}
}

func TestLocalResolveWorkflow(t *testing.T) {
	root := newLocalRepo(t)
	repo := localRepo{root: root}
	abs := filepath.Join(root, ".github", "workflows", "ci.yml")
	for _, arg := range []string{"ci.yml", ".github/workflows/ci.yml", abs} {
		if got := repo.ResolveWorkflow(arg); got != ".github/workflows/ci.yml" {
			t.Errorf("ResolveWorkflow(%q) = %q", arg, got)
		}
	}
	t.Chdir(filepath.Join(root, ".github"))
	if got := repo.ResolveWorkflow("workflows/ci.yml"); got != ".github/workflows/ci.yml" {
		t.Errorf("ResolveWorkflow relative to the working directory = %q", got)
	}
	outside := filepath.Join(t.TempDir(), "other.yml")
	writeFile(t, outside, sampleWorkflow)
	if got := repo.ResolveWorkflow(outside); got != outside {
		t.Errorf("ResolveWorkflow(%q) = %q", outside, got)
	}
	if got := repo.ResolveWorkflow("missing.yml"); got != "missing.yml" {
		t.Errorf("ResolveWorkflow(missing.yml) = %q", got)
	}
}

func TestRemoteRepo(t *testing.T) {
	api := newFakeAPI()
	api.addFile("owner/repo", ".github/workflows/ci.yml", sampleWorkflow)
	api.addFile("owner/repo", ".github/workflows/a.yaml", "name: A\n")
	api.addFile("owner/repo", ".github/workflows/README.md", "")
	api.addFile("owner/repo", "README.md", "")
	repo := &remoteRepo{name: "owner/repo", api: api, shas: map[string]string{}}

	paths, err := repo.WorkflowPaths()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".github/workflows/a.yaml", ".github/workflows/ci.yml"}; !slices.Equal(paths, want) {
		t.Errorf("got %v, want %v", paths, want)
	}

	content, err := repo.ReadWorkflow(".github/workflows/ci.yml")
	if err != nil || string(content) != sampleWorkflow {
		t.Fatalf("got %q, %v", content, err)
	}
	if err := repo.WriteWorkflow(".github/workflows/ci.yml", []byte("updated"), "chore: update"); err != nil {
		t.Fatal(err)
	}
	// Writing a workflow that wasn't read first fetches its SHA.
	if err := repo.WriteWorkflow(".github/workflows/a.yaml", []byte("updated a"), "chore: update"); err != nil {
		t.Fatal(err)
	}
	want := []filePut{
		{"owner/repo", ".github/workflows/ci.yml", "chore: update", "updated", "sha-of-.github/workflows/ci.yml"},
		{"owner/repo", ".github/workflows/a.yaml", "chore: update", "updated a", "sha-of-.github/workflows/a.yaml"},
	}
	if !slices.Equal(api.puts, want) {
		t.Errorf("got puts %v, want %v", api.puts, want)
	}
	if _, err := repo.ReadWorkflow(".github/workflows/missing.yml"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("reading a missing workflow returned %v, want fs.ErrNotExist", err)
	}
}

func TestRemoteRepoWithoutWorkflows(t *testing.T) {
	repo := &remoteRepo{name: "owner/empty", api: newFakeAPI(), shas: map[string]string{}}
	paths, err := repo.WorkflowPaths()
	if err != nil || len(paths) != 0 {
		t.Errorf("got %v, %v; want no paths", paths, err)
	}
}

func TestRemoteResolveWorkflow(t *testing.T) {
	repo := &remoteRepo{name: "owner/repo"}
	tests := map[string]string{
		"ci.yml":                     ".github/workflows/ci.yml",
		".github/workflows/ci.yml":   ".github/workflows/ci.yml",
		"./.github/workflows/ci.yml": ".github/workflows/ci.yml",
	}
	for arg, want := range tests {
		if got := repo.ResolveWorkflow(arg); got != want {
			t.Errorf("ResolveWorkflow(%q) = %q, want %q", arg, got, want)
		}
	}
}

func TestMain(m *testing.M) {
	// Keep tests independent of the developer's environment.
	for _, v := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_HOST", "NO_COLOR", "CLICOLOR_FORCE"} {
		os.Unsetenv(v)
	}
	os.Exit(m.Run())
}
