---
description: |
  Fixes the CI failure a Dependabot upgrade caused, on that pull request's own
  branch. Dispatched by the Dependencies workflow, which disables auto-merge
  first: the fix merges only once CI passes and Copilot has reviewed it.

on:
  workflow_dispatch:
    inputs:
      pr:
        description: Dependabot pull request number
        required: true
        type: string
      branch:
        description: The pull request's head branch
        required: true
        type: string
  # The Dependencies workflow dispatches this with the automation App's token.
  # An App is never a collaborator, so no role matches it; dispatching already
  # requires write access.
  roles: all

engine: claude
timeout-minutes: 30

# Runs queue one at a time; each PR's jobs get their own slot.
concurrency:
  job-discriminator: ${{ github.event.inputs.pr }}

permissions:
  contents: read
  pull-requests: read
  actions: read

checkout:
  ref: ${{ github.event.inputs.branch }}

runtimes:
  go:
    version: "1.26"

network:
  allowed: [defaults, go]

tools:
  github:
    toolsets: [repos, pull_requests, actions]
  bash: true
  edit:

safe-outputs:
  # Pushes as the App so they start CI and Copilot's review (GITHUB_TOKEN's
  # pushes start no workflows).
  github-app:
    client-id: ${{ secrets.AUTOMATION_APP_CLIENT_ID }}
    private-key: ${{ secrets.AUTOMATION_APP_PRIVATE_KEY }}
  push-to-pull-request-branch:
    target: ${{ github.event.inputs.pr }}
    required-labels: [dependencies]
    # The upgrade already changes go.mod and go.sum, and adopting it may need
    # `go mod tidy`. Everything else protected, workflows included, goes to a
    # human as an issue.
    protected-files:
      policy: fallback-to-issue
      exclude: [go.mod, go.sum]
  add-comment:
    target: ${{ github.event.inputs.pr }}
---

# Fix a Dependabot upgrade

Dependabot's pull request #${{ github.event.inputs.pr }} upgrades dependencies
of `${{ github.repository }}`, and its CI failed. Its branch
`${{ github.event.inputs.branch }}` is checked out. Make the code work with the
upgraded versions.

1. Find the failed CI run for the pull request's head commit and read the logs
   of its failed jobs. Read the release notes of the upgraded modules between
   the old and new versions.
2. Reproduce the failure with the commands the failing job runs (see
   `.github/workflows/ci.yml`: golangci-lint at the pinned version, race
   tests, build, govulncheck and the `go mod tidy` check). Follow `AGENTS.md`.
3. Fix the code so it works with the new versions: follow renamed or changed
   APIs, adapt to new behaviour, update tests whose expectations the upgrade
   legitimately changed. Run `go mod tidy` if the module graph needs it.
4. Run the full check again until it passes.
5. Push the fix to pull request #${{ github.event.inputs.pr }} with
   `push_to_pull_request_branch`, in one commit whose message says what the
   upgrade changed and how the code now follows it.
6. Comment on the pull request with the cause and the fix, in a few lines.

Never revert, downgrade or pin back an upgraded dependency, skip or delete a
test, loosen lint, or edit anything under `.github/`. If the upgrade can't be
adopted without a design decision, or the failure has nothing to do with the
upgrade, push nothing: comment on the pull request with what fails and why,
and stop.
