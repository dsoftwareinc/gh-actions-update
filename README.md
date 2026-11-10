# gh actions-update

A [GitHub CLI](https://cli.github.com/) extension that finds outdated actions in your GitHub Actions workflow files and
updates them. It works with both local repository clones and remote GitHub repositories.

> [!NOTE]
> From v2.0.0 on, this extension is written in Go. Up to v1.2.5 it was the `github-actions-cli` Python package, which
> is no longer maintained. The `analyze-orgs` command was dropped in the rewrite; see the [changelog](CHANGELOG.md)
> for the other changes.

## Installation

Requires the [GitHub CLI](https://cli.github.com/) (`gh`).

```shell
gh extension install dsoftwareinc/gh-actions-update
```

This installs a precompiled binary for your platform (Linux, macOS, Windows or FreeBSD) and adds the
`gh actions-update` command.

```shell
# Upgrade to the latest release
gh extension upgrade actions-update

# Uninstall
gh extension remove actions-update
```

It also runs without the GitHub CLI, as the standalone `gh-actions-update` command: download the binary for your
platform from the [releases page](https://github.com/dsoftwareinc/gh-actions-update/releases), or install it with Go:

```shell
go install github.com/dsoftwareinc/gh-actions-update/v2@latest
```

## Quick start

Run inside a local repository clone (or any directory in it) to see which actions have updates available:

```shell
gh actions-update
```

```text
GitHub Actions CLI, scanning repo in /path/to/repo
.github/workflows/publish.yml (Upload Python Package):
	actions/checkout                           v5 ==> v7.0.1
	actions/setup-python                       v6 ==> v7.1.0
	pypa/gh-action-pypi-publish        release/v1
```

Actions with an `==>` have a newer version available. To apply the updates, pass `--update`:

```shell
gh actions-update --update
```

If your workflows pin major tags (`actions/checkout@v5`), add `-m` to keep them that way. Without it, the latest
full version is suggested (`v7.0.1`), and even an up-to-date `@v7` is flagged because `v7.0.1` is newer:

```shell
gh actions-update -m --update
```

```text
.github/workflows/publish.yml (Upload Python Package):
	actions/checkout                           v5 ==> v7
	actions/setup-python                       v6 ==> v7
	pypa/gh-action-pypi-publish        release/v1
```

Actions pinned to a commit SHA are flagged when the commit of the latest release is newer than the pinned one.

## Authentication

If you are logged in with `gh auth login`, there is nothing else to set up: the extension uses your GitHub CLI token.

A token is needed to query the GitHub API for the latest action versions. It is taken from, in order:

1. the `--github-token` option
2. the `GH_TOKEN` or `GITHUB_TOKEN` environment variable
3. the GitHub CLI's logged-in account (`gh auth token`)

Without a token, the extension still works with public repositories, but GitHub allows only 60 unauthenticated API
requests per hour.

Updating a **remote** repository commits to it, so the token needs push access to that repository.

## Commands

### `update-actions` (default)

Lists actions that have updates available. Pass `--update` (`-u`) to apply the changes: local workflow files are
rewritten in place, and remote repositories get one commit per updated workflow.

This is the command that runs when you invoke `gh actions-update` with no subcommand, and it takes the same options
either way.

```shell
# Check for updates in the current repository
gh actions-update

# Apply updates to local workflow files
gh actions-update --update

# Check and update a remote repository
gh actions-update --repo OWNER/REPO --update

# Use a custom commit message when updating a remote repository
gh actions-update --repo OWNER/REPO --update --commit-msg "chore: update actions"
```

```text
Usage:
  gh actions-update update-actions [flags]

Flags:
      --commit-msg string   Commit message, only used for remote repos (default "chore(ci):update actions")
  -h, --help                help for update-actions
  -u, --update              Update actions in workflows (remote repos: commit the changes, local repos: rewrite the files)
```

### `list-workflows`

Lists all workflow files found in a repository.

```shell
gh actions-update list-workflows
gh actions-update --repo OWNER/REPO list-workflows
```

```text
.github/workflows/publish.yml - Publish
.github/workflows/test.yml - Test
```

### `list-actions`

Lists every action referenced in a workflow file, given by its path or its file name in `.github/workflows`.

```shell
gh actions-update list-actions test.yml
gh actions-update --repo OWNER/REPO list-actions .github/workflows/test.yml
```

```text
actions/checkout@v3
actions/setup-python@v4
release-drafter/release-drafter@v5
```

## Global options

Global options can go before or after the command name, for example
`gh actions-update --repo OWNER/REPO -m update-actions`.

```text
Usage:
  gh actions-update [flags]
  gh actions-update [command]

Available Commands:
  help           Help about any command
  list-actions   List actions in a workflow
  list-workflows List workflows in repository
  update-actions Show actions that have updates in repository workflows, and optionally update them

Flags:
      --commit-msg string     Commit message, only used for remote repos (default "chore(ci):update actions")
      --github-token string   GitHub token to use. Defaults to GH_TOKEN or GITHUB_TOKEN, then to "gh auth token"
  -h, --help                  help for gh actions-update
  -m, --major-only            Update major versions only, e.g., v1.2.3 will not be upgraded to v1.2.4 but to v2
      --repo string           Repository to analyze, a local directory or OWNER/REPO (default ".")
  -u, --update                Update actions in workflows (remote repos: commit the changes, local repos: rewrite the files)
  -v, --verbose count         Increase verbosity: -v shows progress, -vv debug details
      --version               version for gh actions-update
```

## Development

Requires Go (see `go.mod` for the version).

```shell
# Run the tests
go test ./...

# Build, then run your build as `gh actions-update`
go build -o gh-actions-update .
gh extension install .
```

Publishing a GitHub release builds the binaries for all platforms and attaches them to it
(`.github/workflows/gh-extension.yml`).
