"""Entry point for `python -m gha_cli` and the precompiled `gh actions-update` extension binary."""

import os
import sys

from gha_cli.cli import cli


def main() -> None:
    # When running as the gh extension binary (gh-actions-update), show usage as `gh actions-update`.
    exe = os.path.splitext(os.path.basename(sys.argv[0]))[0]
    prog_name = "gh actions-update" if exe == "gh-actions-update" else None
    cli(obj={}, prog_name=prog_name)


if __name__ == "__main__":
    main()
