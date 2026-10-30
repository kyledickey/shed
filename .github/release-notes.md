# Writing shed release notes

These notes are read by people who run shed on their own server. They read
them in Settings → Updates before installing, on the GitHub release page, and
in `CHANGELOG.md`. Tell them what changed for them, not how the code changed.

## Format

```markdown
One or two sentences on the highlights of this release.

### Features

#### Deployments

- Added a **Redeploy** button to failed deployments. ([#12](https://github.com/kyledickey/shed/pull/12))

#### Dashboard

- Improved the metric charts' y-axis so it ends at the service's limit. ([a1b2c3d](https://github.com/kyledickey/shed/commit/a1b2c3d))

### Bug Fixes

- Fixed deploys failing with "invalid client id" a while after 1Password was connected. ([#11](https://github.com/kyledickey/shed/pull/11))

### Breaking Changes

- Removed the per-service 1Password links. Connect once in Settings → 1Password to move variables into a vault.
```

- Write no title line; the version heading is added for you.
- Group features under `####` areas, in this order, and leave out empty
  ones: Deployments, Builds, Databases, Variables, Networking, Backups,
  Observability, Dashboard, Agents, Installation and Updates, Other.
- Leave out empty sections. A release with one or two changes may skip
  `### Features` and the areas and be a plain bullet list.
- Put **Breaking Changes** last and include it only when an operator must act
  or something they used is gone. Say what to do.

## Bullets

- One sentence in the past tense, starting with a verb: Added, Improved,
  Changed, Fixed, Removed, Reduced. End with a period.
- Name things the way the dashboard and docs do: Settings → Updates, the
  Variables tab, `shed.toml` keys such as `proxy.cloudflare`.
- Use backticks for config keys, paths, commands, and environment variables.
- Link each bullet to its pull request when the change came from one,
  `([#12](https://github.com/kyledickey/shed/pull/12))`, otherwise to the
  commit that made it, `([a1b2c3d](https://github.com/kyledickey/shed/commit/a1b2c3d))`
  with the 7-character hash. Use links only to this repository.
- Credit outside contributors after the link: `([#12](…); thanks @name)`.
- Merge commits that make one change into one bullet.
- Leave out changes an operator never notices: refactors, tests, CI, docs-only
  wording, design notes, and fixes to features that are new in this release.
- No marketing words ("powerful", "seamless", "exciting"), no emoji.
