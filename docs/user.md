# docs/user.md — Roles, profile, and host preference

Tracked, repo-local source of truth for who is the codeowner and what
role the agent occupies. Replaces the legacy `memory/user_preferences.md`
entry.

## Roles

| Role | Who | Authority |
| --- | --- | --- |
| **Codeowner** | `@yboujraf` (GitHub) = `by-rune` (git author) = `yboujraf@by-systems.be` (email) — same person, three handles | All scope decisions, all PR approvals, all merges, all issue closes. The codeowner approves; the agent never approves itself. Enforced via `.github/CODEOWNERS`. |
| **DevOps (agent)** | Claude (this agent) | Executes the work: branches, builds, tests, atomic commits, pushes, runs probes, drives the testbed. Never opens PRs or merges or closes issues without explicit codeowner "go" / "approuved" / "ok". |

Solo dev project — there are no other humans contributing.

## Codeowner profile

- Org: BY-SYSTEMS SRL
- Workstation: `BY-DESK-03`, Windows 11 Pro, native Go (go1.26.2)
- Editor: VS Code with Dev Containers extension available but unused
- **Never address the codeowner by a first name.** The handle `yboujraf`
  is canonical; do not infer a first name from it.

## Host preference

| Host the agent runs on | Shell to use | Why |
| --- | --- | --- |
| Windows 11 (desk-03) | PowerShell unconditionally | git-bash here spams `/etc/post-install/*.post` permission errors on every command |
| Linux LXC / Devcontainer / WSL | Bash | native shell, clean output |
| macOS | Bash / zsh | native shell |

For file operations the agent prefers the dedicated tools (Glob, Grep,
Read, Edit, Write) regardless of host.

## Test fleet

The agent operates against a separate VLAN (`10.100.0.0/24`) routed via
pfSense. Full inventory + SSH access mesh in [`docs/testbed.md`](testbed.md).

## Vendor reference drivers (NDA)

A secondary reference driver predating this Go rewrite is held under NDA.
The brand identifiers of NDA reference vendors must never appear in
tracked artifacts — see ADR-NNNN once the NDA-vendor-name-policy ADR
lands (pending).

## What the agent does without asking

Per the active DOD window (see ADR-0027 once accepted):

- Branch creation
- Local edits, builds, and unit tests
- Atomic commits (one per tracked issue)
- Push branches to origin for visibility
- Reading code and running probes

## What the agent must NOT do without explicit "go" / "approuved" / "ok"

- `gh pr create` — opening any PR
- `gh pr merge` — merging any PR
- Closing any tracked issue
- Changing scope of an issue
- Force-pushing or amending published commits
- Adding `Closes #N` to a commit body (default is `Refs #N`)
- Uploading content to third-party web services
- Modifying CI / `.github/workflows/`
- Touching shared infrastructure (DNS, Vault, CODEOWNERS)

## Communication style

See ADR-0026 once accepted: terse, tables for facts not options, no A/B/C
menus, progress markers per substep, end-of-turn summary in one or two
sentences.
