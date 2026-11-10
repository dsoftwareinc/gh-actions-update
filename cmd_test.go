package main

import (
	"bytes"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type cmdResult struct {
	stdout, stderr string
	err            error
	tokenUsed      string
	apiCreated     bool
}

// runCmd runs the CLI with args against api. resolvedToken is what token resolution finds without
// --github-token.
func runCmd(t *testing.T, api *fakeAPI, resolvedToken string, args ...string) cmdResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	a := newApp(&stdout, &stderr, false, false)
	a.resolveToken = func(flagToken string) (string, string) {
		if flagToken != "" {
			return flagToken, "github.com"
		}
		return resolvedToken, "github.com"
	}
	var res cmdResult
	a.newAPI = func(token, host string) (githubAPI, error) {
		res.tokenUsed, res.apiCreated = token, true
		return api, nil
	}
	res.err = a.execute(normalizeLegacyArgs(args))
	res.stdout, res.stderr = stdout.String(), stderr.String()
	return res
}

func ciPath(root string) string { return filepath.Join(root, ".github", "workflows", "ci.yml") }

func TestListWorkflowsLocal(t *testing.T) {
	root := newLocalRepo(t)
	writeFile(t, filepath.Join(root, ".github", "workflows", "no-name.yml"), "on: [push]\njobs: {}\n")
	writeFile(t, filepath.Join(root, ".github", "workflows", "broken.yml"), "jobs: [")
	res := runCmd(t, newFakeAPI(), "", "--repo", root, "list-workflows")
	if res.err != nil {
		t.Fatal(res.err)
	}
	want := ".github/workflows/ci.yml - CI\n.github/workflows/no-name.yml - .github/workflows/no-name.yml\n"
	if res.stdout != want {
		t.Errorf("stdout = %q, want %q", res.stdout, want)
	}
	if !strings.Contains(res.stderr, "Skipping .github/workflows/broken.yml") {
		t.Errorf("stderr = %q, want a warning about broken.yml", res.stderr)
	}
	if res.apiCreated || strings.Contains(res.stderr, "token not provided") {
		t.Error("local list-workflows should not need the API")
	}
}

func TestListWorkflowsRemote(t *testing.T) {
	api := newFakeAPI()
	api.addFile("owner/repo", ".github/workflows/ci.yml", sampleWorkflow)
	t.Chdir(t.TempDir())
	res := runCmd(t, api, "token", "--repo", "owner/repo", "list-workflows")
	if res.err != nil {
		t.Fatal(res.err)
	}
	if res.stdout != ".github/workflows/ci.yml - CI\n" {
		t.Errorf("stdout = %q", res.stdout)
	}
}

func TestListActions(t *testing.T) {
	root := newLocalRepo(t)
	for _, arg := range []string{"ci.yml", ".github/workflows/ci.yml", ciPath(root)} {
		res := runCmd(t, newFakeAPI(), "", "--repo", root, "list-actions", arg)
		if res.err != nil {
			t.Fatal(res.err)
		}
		if want := "actions/checkout@v3.5.0\nactions/setup-python@v4.0.0\n"; res.stdout != want {
			t.Errorf("list-actions %s: stdout = %q, want %q", arg, res.stdout, want)
		}
	}
}

func TestListActionsMissingWorkflow(t *testing.T) {
	root := newLocalRepo(t)
	res := runCmd(t, newFakeAPI(), "", "--repo", root, "list-actions", "missing.yml")
	if res.err == nil || !strings.Contains(res.err.Error(), "possible values: .github/workflows/ci.yml") {
		t.Errorf("got error %v", res.err)
	}
}

func TestListActionsRemote(t *testing.T) {
	api := newFakeAPI()
	api.addFile("owner/repo", ".github/workflows/ci.yml", sampleWorkflow)
	t.Chdir(t.TempDir())
	res := runCmd(t, api, "token", "--repo", "owner/repo", "list-actions", "ci.yml")
	if res.err != nil {
		t.Fatal(res.err)
	}
	if want := "actions/checkout@v3.5.0\nactions/setup-python@v4.0.0\n"; res.stdout != want {
		t.Errorf("stdout = %q, want %q", res.stdout, want)
	}
}

func releasesAPI(checkout, setupPython string) *fakeAPI {
	api := newFakeAPI()
	api.addRelease("actions/checkout", checkout, now)
	api.addRelease("actions/setup-python", setupPython, now)
	return api
}

func TestUpdateActionsDryRun(t *testing.T) {
	root := newLocalRepo(t)
	for _, args := range [][]string{{"update-actions"}, {}} {
		res := runCmd(t, releasesAPI("v4.0.0", "v4.0.0"), "token", append([]string{"--repo", root}, args...)...)
		if res.err != nil {
			t.Fatal(res.err)
		}
		want := ".github/workflows/ci.yml (CI):\n" +
			"\tactions/checkout            v3.5.0 ==> v4.0.0\n" +
			"\tactions/setup-python        v4.0.0\n"
		if res.stdout != want {
			t.Errorf("args %v: stdout =\n%s\nwant:\n%s", args, res.stdout, want)
		}
		if got := readFile(t, ciPath(root)); got != sampleWorkflow {
			t.Errorf("dry run modified the workflow:\n%s", got)
		}
	}
}

func TestUpdateActionsUpdatesLocalFiles(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"subcommand", []string{"update-actions", "--update"}},
		{"subcommand short flag", []string{"update-actions", "-u"}},
		{"root command", []string{"-u"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newLocalRepo(t)
			res := runCmd(t, releasesAPI("v4.0.0", "v5.1.0"), "token", append([]string{"--repo", root}, tt.args...)...)
			if res.err != nil {
				t.Fatal(res.err)
			}
			want := strings.NewReplacer("checkout@v3.5.0", "checkout@v4.0.0", "setup-python@v4.0.0", "setup-python@v5.1.0").
				Replace(sampleWorkflow)
			if got := readFile(t, ciPath(root)); got != want {
				t.Errorf("workflow =\n%s\nwant:\n%s", got, want)
			}
			if !strings.Contains(res.stdout, "Updated workflow in .github/workflows/ci.yml") {
				t.Errorf("stdout = %q", res.stdout)
			}
		})
	}
}

func TestUpdateActionsUpToDate(t *testing.T) {
	root := newLocalRepo(t)
	res := runCmd(t, releasesAPI("v3.5.0", "v4.0.0"), "token", "--repo", root, "update-actions", "--update")
	if res.err != nil {
		t.Fatal(res.err)
	}
	if strings.Contains(res.stdout, "==>") || !strings.Contains(res.stdout, "All actions are up to date") {
		t.Errorf("stdout = %q", res.stdout)
	}
	if got := readFile(t, ciPath(root)); got != sampleWorkflow {
		t.Errorf("workflow was modified:\n%s", got)
	}
}

func TestUpdateActionsMajorOnly(t *testing.T) {
	root := newLocalRepo(t)
	res := runCmd(t, releasesAPI("v4.1.2", "v4.2.0"), "token", "--repo", root, "--major-only", "update-actions", "--update")
	if res.err != nil {
		t.Fatal(res.err)
	}
	got := readFile(t, ciPath(root))
	if !strings.Contains(got, "actions/checkout@v4\n") || !strings.Contains(got, "actions/setup-python@v4.0.0\n") {
		t.Errorf("workflow =\n%s", got)
	}
}

func TestUpdateActionsRemoteCommits(t *testing.T) {
	api := releasesAPI("v4.0.0", "v4.0.0")
	api.addFile("owner/repo", ".github/workflows/ci.yml", sampleWorkflow)
	api.addFile("owner/repo", ".github/workflows/other.yml", "jobs:\n  a:\n    steps:\n      - uses: actions/setup-python@v4.0.0\n")
	t.Chdir(t.TempDir())
	res := runCmd(t, api, "token", "--repo", "owner/repo", "update-actions", "-u", "-commit-msg", "ci: bump actions")
	if res.err != nil {
		t.Fatal(res.err)
	}
	// other.yml is up to date, so only ci.yml gets a commit.
	if len(api.puts) != 1 {
		t.Fatalf("got %d commits, want 1: %v", len(api.puts), api.puts)
	}
	put := api.puts[0]
	if put.path != ".github/workflows/ci.yml" || put.message != "ci: bump actions" ||
		put.sha != "sha-of-.github/workflows/ci.yml" || !strings.Contains(put.content, "actions/checkout@v4.0.0") {
		t.Errorf("got commit %+v", put)
	}
	if !strings.Contains(res.stdout, "Committed changes to workflow in owner/repo:.github/workflows/ci.yml") {
		t.Errorf("stdout = %q", res.stdout)
	}
}

func TestUpdateActionsSkipsLocalAndDockerActions(t *testing.T) {
	root := newLocalRepo(t)
	writeFile(t, ciPath(root), `name: CI
jobs:
  build:
    steps:
      - uses: ./.github/actions/setup
      - uses: docker://alpine:3.20
      - uses: actions/checkout@v3.5.0
`)
	api := releasesAPI("v4.0.0", "v4.0.0")
	res := runCmd(t, api, "token", "--repo", root)
	if res.err != nil {
		t.Fatal(res.err)
	}
	if want := ".github/workflows/ci.yml (CI):\n\tactions/checkout        v3.5.0 ==> v4.0.0\n"; res.stdout != want {
		t.Errorf("stdout = %q, want %q", res.stdout, want)
	}
}

func TestUpdateActionsNoWorkflows(t *testing.T) {
	res := runCmd(t, newFakeAPI(), "token", "--repo", t.TempDir())
	if res.err != nil {
		t.Fatal(res.err)
	}
	if !strings.Contains(res.stderr, "No workflows found") {
		t.Errorf("stderr = %q", res.stderr)
	}
}

func TestTokenResolution(t *testing.T) {
	root := newLocalRepo(t)

	res := runCmd(t, releasesAPI("v4.0.0", "v4.0.0"), "", "--repo", root, "update-actions")
	if res.err != nil {
		t.Fatal(res.err)
	}
	if !strings.Contains(res.stderr, "GitHub connection token not provided") || res.tokenUsed != "" {
		t.Errorf("without a token: stderr = %q, token = %q", res.stderr, res.tokenUsed)
	}

	res = runCmd(t, releasesAPI("v4.0.0", "v4.0.0"), "gh-cli-token", "--repo", root, "update-actions")
	if strings.Contains(res.stderr, "token not provided") || res.tokenUsed != "gh-cli-token" {
		t.Errorf("with a resolved token: stderr = %q, token = %q", res.stderr, res.tokenUsed)
	}

	res = runCmd(t, releasesAPI("v4.0.0", "v4.0.0"), "gh-cli-token", "--repo", root, "--github-token", "flag-token")
	if res.tokenUsed != "flag-token" {
		t.Errorf("--github-token: token = %q", res.tokenUsed)
	}
}

func TestVerbosity(t *testing.T) {
	root := newLocalRepo(t)
	res := runCmd(t, releasesAPI("v4.0.0", "v4.0.0"), "token", "--repo", root)
	if strings.Contains(res.stderr, "INFO") {
		t.Errorf("INFO logs shown without -v: %q", res.stderr)
	}
	res = runCmd(t, releasesAPI("v4.0.0", "v4.0.0"), "token", "--repo", root, "-v")
	if !strings.Contains(res.stderr, "INFO Getting latest release for repository: actions/checkout") ||
		strings.Contains(res.stderr, "DEBUG") {
		t.Errorf("-v: stderr = %q", res.stderr)
	}
}

func TestUnknownCommand(t *testing.T) {
	res := runCmd(t, newFakeAPI(), "token", "analyze-orgs")
	if res.err == nil || !strings.Contains(res.err.Error(), `unknown command "analyze-orgs"`) {
		t.Errorf("got %v", res.err)
	}
}

func TestNormalizeLegacyArgs(t *testing.T) {
	got := normalizeLegacyArgs([]string{"update-actions", "-commit-msg", "msg", "-commit-msg=x", "--", "-commit-msg"})
	want := []string{"update-actions", "--commit-msg", "msg", "--commit-msg=x", "--", "-commit-msg"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestVersion(t *testing.T) {
	res := runCmd(t, newFakeAPI(), "", "--version")
	if want := "gh actions-update " + currentVersion + "\n"; res.err != nil || res.stdout != want {
		t.Errorf("got %q, %v; want %q", res.stdout, res.err, want)
	}
}
