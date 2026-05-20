# 1. Docker-compose profile support

## Status

Proposed.

## Context

[Issue #423](https://github.com/jesseduffield/lazydocker/issues/423) requests that lazydocker understand docker-compose [profiles](https://docs.docker.com/compose/profiles/). Today it shows every service in the project, with no way to scope to a specific profile and no way to invoke profile-aware `up`/`down`/`restart`.

A community PR — [#638](https://github.com/jesseduffield/lazydocker/pull/638), branch `saravanabalagi/feat/docker_profiles` — attempted this in March 2025 and has sat open since. Two problems with picking it up:

1. **Master moved on substantially.** Since the PR was authored, master has gained multi-project discovery from container labels (`com.docker.compose.project`), a project-filter on the services and containers panels, a `Hide` on the project panel keyed off `IsProjectScoped()`, and a `-p` flag injection in `NewCommandObject`. The PR's structural changes overlap with those refactors; merging or rebasing produces large, gnarly conflicts in `project_panel.go`.
2. **The PR has a latent bug.** It adds a `CheckDockerComposeProfiles` template that uses a shell pipeline (`... | grep -q ...; echo $?`), but `OSCommand.RunCommand` parses commands with `str.ToArgv` and exec's directly — no shell. So the precheck always fails, `HasProfiles` stays false, and the UI never shows profile rows even though the rest of the feature is wired up. Anyone testing the branch sees "nothing happens".

The cleanest path is to redesign on master rather than rescue the PR. Master's `getDiscoveredProjects()` already gives us a list-of-rows model in the project panel; profiles slot in as additional rows of the same `*commands.Project` type. And master's `NewCommandObject` already injects `-p <name>` from a `Project`/`Service` field — we can extend the same mechanism to inject `--profile <name>`, removing the need for the PR's separate `UpProfile`/`DownProfile`/... template family.

## Decision

Implement profile support as an additive change on master, with the following architectural choices:

### Profile is a flag on `Project`, not a new type

Add `IsProfile bool` to `pkg/commands/project.go`'s `Project` struct. Profiles render as pseudo-projects in the panel. This avoids a generic-type split across the `SideListPanel[*commands.Project]`, `CommandObject`, presentation, services and containers panels — all of which already pass `*Project` around.

### Reuse existing command templates; inject `--profile X` via `NewCommandObject`

Add a `Profile string` field to `CommandObject` and have `NewCommandObject` append `--profile X` (in addition to the existing `-p <name>` for the local project) when a profile context is set. This means the existing `Up`, `Down`, `DownWithVolumes`, `AllLogs`, `DockerComposeConfig` templates work unchanged for profile invocations — no `UpProfile` / `DownProfile` / `RestartProfile` / `AllLogsProfile` template family. The single new template is `Restart` (master doesn't have a project-level restart at all today).

This is the key reason the change is small. The original PR added ~12 new templates and ~12 corresponding i18n strings; this design needs one new template and a handful of i18n strings (for confirmation/status messages on profile actions).

### Discovery via shelling out, cached for the session

Add `GetProfiles()` and `GetProfileServices(profile)` methods on `DockerCommand` that shell out to `docker compose config --profiles` and `docker compose --profile X config --services` respectively. Cache results on the `DockerCommand` struct under a mutex; never invalidate during a session. Compose files rarely change mid-session, and `GetServices()` already has the same property — users restart lazydocker to pick up changes.

**No precheck.** The PR's `CheckDockerComposeProfiles` template (which was the bug source) is unnecessary — we just enumerate. Empty result → no profile rows. This also means no shell-pipe escape hatch is needed; everything stays compatible with `OSCommand.RunCommand`'s argv-exec model.

### Soft-fail discovery

If `GetProfiles()` errors (e.g. `docker compose` is missing, compose v1 too old), log once and cache empty. No popup, no warning row. The UI degrades to "no profile rows" — same UX as a compose project with no profiles declared. This matches master's existing `GetServices()` behaviour.

### Selecting a profile filters the services and containers panels

Master's project filter on `services_panel.go` and `containers_panel.go` keeps items where `ProjectName == selectedProject`. Add a profile branch: when a profile is selected, keep items where `ProjectName == LocalProjectName` AND `ServiceName ∈ GetProfileServices(profile)`. The set returned by `docker compose --profile X config --services` includes the profile's services plus the default (no-profile) services — that's docker's own semantics and matches what users expect ("show me what would be running if I activated this profile").

### Profiles render after projects in the panel

Sort callback returns non-profiles first, then alphabetical within each group. No per-project grouping (profiles always belong to the local project, of which there is exactly one). Selection restore keys on the `(Name, IsProfile)` tuple so a profile that happens to share a project's name doesn't cross-select.

### Visual: cyan "profile" prefix label

Mirrors the existing two-column row layout. Re-uses `utils.ColoredString(..., color.FgCyan)`.

### English-only i18n initially

Add new strings to `pkg/i18n/english.go`. If `go build ./...` shows other locales fail (they shouldn't — they inherit empty strings for new fields by default), backfill defaults; otherwise leave localization for a follow-up.

### Scope limits

- No new keybinding for project/profile restart in this PR — leave for maintainer discussion. The handler exists; binding it is one line.
- No profile-aware custom service/container commands. A service belongs to a specific project, not a profile; threading profile context into per-service custom commands would change semantics surprisingly.
- No cache invalidation on compose-file change. Defer until requested.

## Consequences

### Positive

- **Small diff.** ~80-100 lines added across 5-6 files, vs. the PR's ~350 lines. Easier review, easier maintenance.
- **No new template family.** `Up`/`Down`/`Restart`/`AllLogs`/`DockerComposeConfig` work unchanged for profile invocations because `--profile X` is injected at the `NewCommandObject` layer. Users who've customized these templates get profile support for free.
- **No latent shell-pipe bug.** No precheck template; no shell-metacharacter dependencies in `OSCommand.RunCommand`.
- **Slots into master's existing architecture.** Profiles are just more rows; selection still flows through `OnSelect`; filter still flows through the existing `Filter` callback. No new panel, no new lifecycle.
- **PR-able cleanly upstream.** Additive across well-bounded extension points.

### Negative / risks

- **Re-shells `docker compose config` once per session.** Both `--profiles` (once) and `--profile X --services` (once per profile selected). Both can be slow on large compose files. Mitigated by single-call caching.
- **`Restart` template is new.** Users with a heavily customized `~/.config/lazydocker/config.yml` that wipes defaults will need to add it. Default value is `"{{ .DockerCompose }} restart"` — boring and obvious.
- **Selection-restore tuple change.** If anything downstream of `refreshProject` assumes project names are unique within the panel, it breaks. Audit shows nothing relies on this today, but worth noting in PR review.
- **No per-profile cache invalidation.** If a user edits `compose.yml` to add a new profile mid-session, lazydocker won't see it until restart. Same constraint as `GetServices()`.
- **Profile-aware filtering issues an extra shell call per profile selection.** At 100ms refresh tick and cached results, this is one shell per profile per session — negligible.

### Risks if rejected

If upstream wants separate `UpProfile`/etc. templates for customization symmetry with the existing pattern, the design has to grow back toward the original PR's shape. Worth raising in PR discussion before sinking time into the larger version.

## References

- Issue: [jesseduffield/lazydocker#423](https://github.com/jesseduffield/lazydocker/issues/423)
- Prior PR (stale): [jesseduffield/lazydocker#638](https://github.com/jesseduffield/lazydocker/pull/638)
- Implementation plan: see this repo's branch and corresponding PR.
