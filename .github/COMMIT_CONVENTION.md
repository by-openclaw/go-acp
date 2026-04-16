# Commit Message Convention

```
<type>(<scope>): <subject>

[optional body]

[optional footer]
```

## Types

| Type | Use for |
|------|---------|
| `feat` | new feature |
| `fix` | bug fix |
| `docs` | documentation only |
| `test` | adding or fixing tests |
| `chore` | maintenance, refactor, CI |
| `security` | security fix or hardening |

## Scopes

| Scope | Package |
|-------|---------|
| `acp1` | `internal/protocol/acp1/` |
| `acp2` | `internal/protocol/acp2/` |
| `transport` | `internal/transport/` |
| `export` | `internal/export/` |
| `cli` | `cmd/acp/` |
| `api` | `cmd/acp-srv/`, `api/` |
| `core` | `internal/protocol/` (shared types) |
| `ci` | `.github/workflows/` |

## Examples

```
feat(acp1): add TCP direct transport
fix(acp1): handle empty enum item list from emulator
docs(cli): add export examples to help text
test(acp2): add spec-compliance tests for property alignment
chore(ci): add golangci-lint to workflow
```

## Rules

- Subject: imperative mood, no period, under 72 chars
- Body: wrap at 72 chars, explain WHY not WHAT
- Footer: `Closes #123` to auto-close issues
- Breaking changes: `BREAKING CHANGE:` in footer
