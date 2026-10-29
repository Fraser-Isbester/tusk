---
applyTo: "**/go.mod,**/go.sum,.github/workflows/*.yml"
excludeAgent: "coding-agent"
---

These files change mostly in Dependabot PRs, which merge without a human once
your review is in and CI passes. Review the upgrade, not the diff:

- Read each upgraded module's release notes between the old and new version.
  Flag breaking changes, deprecations and behaviour changes that touch how
  this repository uses it.
- Flag a major version bump, a module that changed owner or was renamed, a
  licence change, and any new direct or indirect dependency.
- For actions, flag a pin that moves to a different owner or from a commit
  SHA to a mutable tag.
- Commits on the PR that are not Dependabot's are an agent's fix for a CI
  failure the upgrade caused. Check that they adapt the code to the new
  version rather than working around it (reverting the upgrade, skipping
  tests, loosening lint).

Be brief. Do not restate the diff or the release notes; comment only where
something needs a human.
