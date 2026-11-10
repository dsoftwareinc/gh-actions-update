# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build (the binary must be named gh-actions-update for `gh extension install .`)
go build -o gh-actions-update .

# Run the CLI
go run . --help

# Test (add -race to check the concurrent lookups)
go test ./...

# Lint and format
go vet ./...
gofmt -l -w .

# Install the local build as the `gh actions-update` extension
gh extension install .
```

## Architecture

This is a Go [GitHub CLI](https://cli.github.com/) extension, `gh actions-update`, that finds and updates outdated
actions in GitHub Actions workflows. Everything is in `package main` at the repository root; the module path is
`github.com/dsoftwareinc/gh-actions-update/v2` (it has to end in `/v2` while release tags are v2.x).

- **`main.go`**: entry point. Sets up colors from the terminal (go-gh `term`), holds the version
  (`currentVersion`, overridden by `-X main.version=<tag>` in release builds), and rewrites the legacy
  `-commit-msg` flag to `--commit-msg`.
- **`cmd.go`**: cobra commands. `app` holds the global flags and creates the GitHub API client lazily (`app.github`),
  so local-only commands never resolve a token. The root command runs `update-actions` and accepts its flags too.
  Subcommands: `update-actions`, `list-actions`, `list-workflows`.
- **`repo.go`**: the `repository` interface with two implementations. `localRepo` reads/writes files under the git
  root found by walking up from `--repo`; `remoteRepo` (`--repo OWNER/REPO`) goes through the API and commits each
  updated workflow. Workflow paths are always slash-separated and relative to the repo root.
- **`updater.go`**: `updater` resolves the latest release of each `uses:` reference, with bounded concurrency and a
  `memo` cache so each release and commit is fetched once. SemVer-ish tags are compared numerically
  (`compareVersions`, `--major-only` compares majors only); 40-char SHA pins are compared by commit date against the
  commit of the latest release.
- **`workflow.go`**: YAML parsing (`steps[].uses` in file order) and `applyUpdates`, which rewrites whole
  `name@version` references only.
- **`github.go`**: the `githubAPI` interface and its REST implementation on go-gh's client. Without a token it uses
  `anonymousClient`, because go-gh refuses to build a client without one. Missing resources map to `errNotFound`.

Tests use `fakeAPI` (`fake_test.go`) in place of GitHub; `github_test.go` covers the REST layer with `httptest`.

### Authentication

`--github-token`, then go-gh's `auth.TokenForHost` (`GH_TOKEN`/`GITHUB_TOKEN`, gh's config, `gh auth token`). Without
a token the tool warns and makes unauthenticated requests.

### Release

Bump `currentVersion` in `main.go` and add a `CHANGELOG.md` entry, then publish a GitHub release tagged with that
version. `.github/workflows/gh-extension.yml` then runs: it cross-compiles with `CGO_ENABLED=0` and
uploads `gh-actions-update-<os>-<arch>[.exe]` assets, which `gh extension install` picks by platform (the repo must
stay named `gh-actions-update`).
