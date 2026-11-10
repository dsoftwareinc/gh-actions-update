# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Install dependencies
uv sync

# Run the CLI
uv run github-actions-cli

# Lint
uv run ruff check .

# Format
uv run ruff format .

# Build package
python -m build
```

## Architecture

This is a Python CLI tool (`github-actions-cli`) for managing GitHub Actions workflow versions. The entry point is `gha_cli/cli.py`, registered as `github-actions-cli` in `pyproject.toml`.

### Core design

**`gha_cli/cli.py`** — Everything lives here:
- `GithubActionsTools` class wraps PyGitHub and drives all workflow operations. It caches workflow files (`_wf_cache`) and latest releases (`__actions_latest_release`) to avoid redundant API calls.
- **Dual-mode**: The class transparently handles both local repos (detected via `os.path.exists(repo) and .git/` exists) and remote GitHub repos (in `owner/repo` format). Local repos read/write files directly; remote repos use the GitHub API.
- `get_action_latest_release` handles two version formats: SemVer tags (compared numerically) and SHA pinned versions (compared by commit timestamp vs. release timestamp).
- Click group `cli` is the root command; `update_actions`, `list_actions`, `list_workflows`, and `analyze_orgs` are subcommands. When invoked without a subcommand, `update_actions` runs by default.

**`gha_cli/scanner.py`** — Data classes (`Org`, `Repo`) for the `analyze_orgs` command. They extend `CsvClass` which provides generic CSV serialization via dataclass field introspection. Fields listed in `IGNORE_FIELDS` are excluded from CSV output.

### gh extension

`gha_cli/__main__.py` is the entry point for the PyInstaller binaries built by `.github/workflows/gh-extension.yml` on each release, uploaded as `gh-actions-update-<os>-<arch>[.exe]` so the repo installs as the `gh actions-update` precompiled extension (requires the repo to be named `gh-actions-update`).

### Authentication

`--github-token`, then `GITHUB_TOKEN`/`GH_TOKEN`, then `gh auth token` (`_get_gh_cli_token`) is required for remote repos and for writing. Without it the tool still works read-only against local repos.
