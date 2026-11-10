## v1.2.5

### 🚀 Features

- Distribute as a precompiled GitHub CLI extension: `gh extension install dsoftwareinc/gh-actions-update`
- Fall back to `GH_TOKEN` and then `gh auth token` when `GITHUB_TOKEN` is not set
- Add `python -m gha_cli` entry point

### 🐛 Bug Fixes

- fix: `--major-only` wrote the full version (e.g. `v7.0.1`) into the first workflow using an action instead of the major tag (`v7`)
- fix: An action pinned to a commit SHA was only flagged as outdated the first time it appeared; later workflows using it showed no update
- fix: A SHA pin that doesn't exist in the action's repository no longer crashes the scan

## v1.2.4

### 🐛 Bug Fixes

- fix: Support action names with sub-paths (e.g. `actions/upload-artifact/merge`) #8
- fix: UnicodeDecodeError reading workflow files on Windows — always open with UTF-8 encoding #5
- fix: UnboundLocalError when a repository has no releases
- fix: PyGitHub deprecation warning — use `Auth.Token` instead of `login_or_token`

### 🧪 Tests

- Add test suite with pytest and pytest-mock

## v1.2.3

- Migrate to uv

### 🐛 Bug Fixes

- fix: Comparison can be timezone aware

## v1.2.2

### 🐛 Bug Fixes

- fix: Update versions
- fix: error when repository is not found

## v1.2.1

### 🚀 Features

- Support for updating major action versions only with `--major-only` flag.

## v1.1.6

### 🐛 Bug Fixes

- fix: Workflow path is enumerated, but does not exist any longer #4 @amotl

## v1.1.1

### 🚀 Features

- Format output string to be more readable.
- Scan and analyze GitHub organizations and repositories.

## v1.1.0

### 🚀 Features

- Compare only major versions of the actions by default.

## v1.0.1

### 🚀 Features

- Support default action to list actions requiring update without updating.

## v1.0.0

### 🚀 Features

- Update GitHub actions to their latest release tag on local repo or remote repo.

<!--
### 🐛 Bug Fixes
### 🚀 Features
-->