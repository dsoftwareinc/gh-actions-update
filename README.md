# gh actions-update

A [GitHub CLI](https://cli.github.com/) extension that finds outdated actions in your GitHub Actions workflow files and
updates them. It works with both local repository clones and remote GitHub repositories.

> [!NOTE]
> This project was previously distributed only as the `github-actions-cli` Python package, and the repository was
> renamed from `github-actions-cli` to `gh-actions-update`. From v1.2.5 on, the GitHub CLI extension is the recommended
> way to use it. The Python package still works; see [Using the Python package](#using-the-python-package).

## Installation

Requires the [GitHub CLI](https://cli.github.com/) (`gh`).

```shell
gh extension install dsoftwareinc/gh-actions-update
```

This installs a precompiled binary for your platform (Linux, macOS or Windows, no Python needed) and adds the
`gh actions-update` command.

```shell
# Upgrade to the latest release
gh extension upgrade actions-update

# Uninstall
gh extension remove actions-update
```

## Quick start

Run inside any local repository clone to see which actions have updates available:

```shell
gh actions-update
```

```text
GitHub Actions CLI, scanning repo in /path/to/repo
.github/workflows/publish.yml (Upload Python Package):
	actions/checkout                           v5 ==> v7.0.1
	pypa/gh-action-pypi-publish        release/v1
	actions/setup-python                       v6 ==> v7.1.0
```

Actions with an `==>` have a newer version available. To apply the updates, name the `update-actions` command and
pass `--update`:

```shell
gh actions-update update-actions --update
```

If your workflows pin major tags (`actions/checkout@v5`), add `-m` to keep them that way. Without it, the latest
full version is suggested (`v7.0.1`), and even an up-to-date `@v7` is flagged because `v7.0.1` is newer:

```shell
gh actions-update -m update-actions --update
```

```text
.github/workflows/publish.yml (Upload Python Package):
	actions/checkout                           v5 ==> v7
	pypa/gh-action-pypi-publish        release/v1
	actions/setup-python                       v6 ==> v7
```

## Authentication

If you are logged in with `gh auth login`, there is nothing else to set up: the extension uses your GitHub CLI token.

A token is needed to query the GitHub API for the latest action versions. It is taken from, in order:

1. the `--github-token` option
2. the `GITHUB_TOKEN` or `GH_TOKEN` environment variable
3. `gh auth token`, the GitHub CLI's logged-in account

Updating a **remote** repository commits to it, so the token needs push access to that repository.

## Commands

### `update-actions` (default)

Lists actions that have updates available. Pass `--update` (`-u`) to apply the changes: local workflow files are
rewritten in place, and remote repositories get a commit.

This is the command that runs when you invoke `gh actions-update` with no subcommand, but its options belong to the
subcommand: to pass `--update` you have to spell out `update-actions`.

```shell
# Check for updates in the current directory
gh actions-update

# Apply updates to local workflow files
gh actions-update update-actions --update

# Check and update a remote repository
gh actions-update --repo OWNER/REPO update-actions --update

# Use a custom commit message when updating a remote repository
gh actions-update --repo OWNER/REPO update-actions --update -commit-msg "chore: update actions"
```

```text
Usage: gh actions-update update-actions [OPTIONS]

  Show actions required updates in repository workflows

Options:
  -u, --update      Update actions in workflows (For remote repos: make
                    changes and commit, for local repos: update files
  -commit-msg TEXT  Commit msg, only relevant when remote repo  [default:
                    chore(ci):update actions]
  --help            Show this message and exit.
```

### `list-workflows`

Lists all workflow files found in a repository.

```shell
gh actions-update list-workflows
gh actions-update --repo OWNER/REPO list-workflows
```

```text
.github/workflows/test.yml - Test
.github/workflows/publish.yml - Publish
```

### `list-actions`

Lists every action referenced in a specific workflow file.

```shell
gh actions-update list-actions .github/workflows/test.yml
gh actions-update --repo OWNER/REPO list-actions .github/workflows/test.yml
```

```text
actions/checkout@v3
actions/setup-python@v4
release-drafter/release-drafter@v5
```

### `analyze-orgs`

Scans all GitHub organizations the authenticated user belongs to and prints a CSV summary of their repositories.

```shell
gh actions-update analyze-orgs
gh actions-update analyze-orgs --exclude my-org --exclude another-org
```

## Global options

Global options go before the command name, for example `gh actions-update --repo OWNER/REPO -m update-actions`.

```text
Usage: gh actions-update [OPTIONS] [COMMAND] [ARGS]...

Options:
  -v, --verbose        Increase verbosity, can be used multiple times to
                       increase verbosity
  --repo TEXT          Repository to analyze, can be a local directory or a
                       {OWNER}/{REPO} format  [default: .]
  --github-token TEXT  GitHub token to use. Defaults to GITHUB_TOKEN or
                       GH_TOKEN, then to `gh auth token`
  -m, --major-only     Update major versions only, e.g., v1.2.3 will not be
                       upgraded to v1.2.4 but to v2
  --help               Show this message and exit.

Commands:
  analyze-orgs    Analyze organizations
  list-actions    List actions in a workflow
  list-workflows  List workflows in repository
  update-actions  Show actions required updates in repository workflows
```

## Using the Python package

The same tool is published on PyPI as `github-actions-cli`, for environments without the GitHub CLI. It needs
Python 3.10 or newer.

```shell
pip install github-actions-cli

# or run it without installing
uvx github-actions-cli
```

It takes exactly the same options and commands; use `github-actions-cli` wherever this README says
`gh actions-update`:

```shell
github-actions-cli -m update-actions --update
```

Without the GitHub CLI logged in, set `GITHUB_TOKEN` (or pass `--github-token`) to give it a token.
