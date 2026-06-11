# Release Process

The MCP Lifecycle Operator is released on an as-needed basis. The process is as follows:

## Issue Templates

Two GitHub issue templates are provided under [`.github/ISSUE_TEMPLATE/`](.github/ISSUE_TEMPLATE/):

- [**new-release.md**](.github/ISSUE_TEMPLATE/new-release.md) - for major/minor releases (creates a release branch, runs
  the full checklist)
- [**new-patch-release.md**](.github/ISSUE_TEMPLATE/new-patch-release.md) - for patch releases (cherry-picks to an existing
  release branch)

See `docs/release-v0.1.0-issue.md` for a concrete example of a completed
release issue.

## Process

Open a release issue using the appropriate template above. The template
contains the complete step-by-step checklist for the release, including branch
management, CI verification, image promotion, and artifact generation.

All [OWNERS](OWNERS) must LGTM the release proposal before proceeding.
