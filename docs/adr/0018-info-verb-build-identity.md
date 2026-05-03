# ADR-0018 — `info` verb build identity

Status: accepted

## Context

Operations and support need to know exactly which build is running
in the field: version tag, commit SHA, commit date, build date, OS,
arch, current license status. Bug reports without this are
unreproducible.

## Decision

Every connector binary's `info` verb (per ADR-0002) reports build
identity in JSON format.

### Output

```
$ dhs-emberplus info --output json
{
  "name":          "dhs-emberplus",
  "version":       "v0.7.1",
  "commit":        "0cbde05a3f2c1d4e6b8a9c7e5d3f2a1b0c9d8e7f",
  "commit_short":  "0cbde05",
  "commit_date":   "2026-05-03T14:32:00Z",
  "commit_url":    "https://github.com/by-openclaw/dhs-emberplus/commit/0cbde05a3f2c1d4e6b8a9c7e5d3f2a1b0c9d8e7f",
  "build_date":    "2026-05-03T15:00:00Z",
  "build_host":    "build-01.by-systems.internal",
  "go_version":    "go1.23.4",
  "os":            "linux",
  "arch":          "amd64",
  "dnssd_backend": "avahi-dbus",
  "license_status": "valid_until_2027-05-03",
  "features_enabled": ["consumer", "provider"]
}
```

### Fields

| Field | Source |
|---|---|
| `name` | binary name (compile-time `-X`) |
| `version` | git tag at build time (or `dev` if untagged) |
| `commit` | full SHA (`git rev-parse HEAD`) |
| `commit_short` | derived (`commit[:7]`) at runtime |
| `commit_date` | `git log -1 --format=%cI` |
| `commit_url` | template-substituted from `RepoURL` + `commit` |
| `build_date` | `date -Iseconds -u` at build time |
| `build_host` | `hostname` at build time |
| `go_version` | `runtime.Version()` |
| `os` / `arch` | `runtime.GOOS` / `runtime.GOARCH` |
| `dnssd_backend` | runtime selection (per ADR-0016) |
| `license_status` | runtime — verifies cached license |
| `features_enabled` | runtime — from license claims (ADR-0003) |

### Implementation

Shared package per connector module:

```go
// internal/buildinfo/buildinfo.go
package buildinfo

var (
    Version    string  // -ldflags "-X internal/buildinfo.Version=v0.7.1"
    Commit     string  // -ldflags "-X internal/buildinfo.Commit=<sha>"
    CommitDate string  // -ldflags "-X internal/buildinfo.CommitDate=<iso8601>"
    BuildDate  string  // -ldflags "-X internal/buildinfo.BuildDate=<iso8601>"
    BuildHost  string  // -ldflags "-X internal/buildinfo.BuildHost=<host>"
    RepoURL    string  // -ldflags "-X internal/buildinfo.RepoURL=https://github.com/by-openclaw/dhs-<proto>"
)
```

### Build-time injection (Makefile / CI)

```makefile
LDFLAGS := \
  -X internal/buildinfo.Version=$(VERSION) \
  -X internal/buildinfo.Commit=$(shell git rev-parse HEAD) \
  -X internal/buildinfo.CommitDate=$(shell git log -1 --format=%cI) \
  -X internal/buildinfo.BuildDate=$(shell date -Iseconds -u) \
  -X internal/buildinfo.BuildHost=$(shell hostname) \
  -X internal/buildinfo.RepoURL=https://github.com/by-openclaw/dhs-$(PROTO)
```

## Consequences

- Bug reports include exact build identity.
- Support can match deployed assets to release notes.
- `dhs.asset.event` records (per ADR-0011) include `dhs_version` +
  `commit` for fleet-wide visibility.
- Customer audit answers ("which version is running on which host?")
  trace to one JSON call.

## Forbidden

- Reporting a missing field (every field MUST be populated; "dev"
  / "unknown" allowed only in unstamped local builds, never in
  shipped binaries).
- Using runtime stat calls (file mtime, etc.) to fake build dates.
- Different field names per connector (the schema is identical
  across every binary).
