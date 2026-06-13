# runbook.md — dhs developer runbook

Step-by-step guide to set up the dev environment, build, test, and release.

Two supported workflows:

- **Devcontainer (recommended)** — everything in a Linux container, one-time
  install on Windows. Reproducible, no host pollution.
- **Native Windows** — Go installed directly on Windows. Required for
  talking to real devices over UDP broadcast (devcontainers NAT UDP).

Pick one. The commands in each section are labelled **[container]**,
**[windows]**, or **[both]**.

---

## 1. One-time setup

### 1a. Devcontainer workflow (recommended)

**Prereqs on Windows 11**:

```powershell
# Pick ONE container runtime:
winget install --id Docker.DockerDesktop -e          # easiest, needs Hyper-V + a WSL2 VM internally
winget install --id SUSE.RancherDesktop -e           # alternative, Hyper-V backend
winget install --id RedHat.Podman-Desktop -e         # alternative, rootless

# VS Code + Dev Containers extension:
winget install --id Microsoft.VisualStudioCode -e
code --install-extension ms-vscode-remote.remote-containers
```

Start your chosen runtime (Docker Desktop / Rancher / Podman Desktop), wait
for it to report "running", then:

```powershell
cd C:\Users\BY-SYSTEMSSRLBoujraf\Downloads\acp
code .
```

VS Code will pop a toast: **"Reopen in Container"**. Click it. First build
takes 3–5 min (pulls Go image, installs tools via `.devcontainer/post-create.sh`).
You land in a bash shell inside the container at `/workspaces/acp`.

Verify:

```bash
go version        # go1.23.x or newer
git --version
golangci-lint --version
```

### 1b. Native Windows workflow

```powershell
winget install --id GoLang.Go -e
winget install --id Git.Git -e
winget install --id golangci-lint.golangci-lint -e
winget install --id GnuWin32.Make -e              # for `make` targets
winget install --id WiresharkFoundation.Wireshark -e
```

**Open a new shell** after installing so PATH refreshes. Verify:

```powershell
go version
make --version
```

Then open the repo:

```powershell
cd C:\Users\BY-SYSTEMSSRLBoujraf\Downloads\acp
go mod tidy
```

---

## 2. Build

| Target           | Command                                         | Produces                             |
|------------------|-------------------------------------------------|--------------------------------------|
| CLI binary       | `make build` or `go build -o bin/dhs ./cmd/dhs` | `bin/dhs` (`bin\dhs.exe` on Windows) |
| Plain `go build` | `go build ./...`                                | cached, no output files              |

---

## 3. Test

### 3a. Unit tests (fast, no device required)

Always safe to run. Uses in-memory mock transport, byte-exact against spec.

```bash
make test              # or: go test ./...
make test-race         # or: go test -race ./...
make test-cover        # or: go test -cover ./...
```

CI runs unit tests on every commit.

### 3b. Integration tests (require a real device or emulator)

Tagged `//go:build integration`. Skipped unless env vars are set.

```bash
# ACP1 emulator or real device on your LAN:
export ACP1_TEST_HOST=192.168.1.5
make test-integration-acp1

# ACP2 device:
export ACP2_TEST_HOST=192.168.1.8
make test-integration-acp2

# Both:
make test-integration
```

**[container]** UDP broadcast traffic does NOT cleanly reach a devcontainer
on Windows (NAT). Integration tests that exercise `discover` must run
**[windows]** natively. Unit tests and direct-connect integration tests
(unicast UDP/TCP) work from inside the container if you publish the device
IP — but broadcast announce reception does not.

### 3c. Lint + vet

```bash
make lint              # golangci-lint run ./...
make vet               # go vet ./...
make fmt-check         # goimports -l (non-zero exit if any file needs formatting)
```

CI runs all three.

---

## 4. Run

After `make build`:

```bash
./bin/dhs consumer acp1 discover
./bin/dhs consumer acp1 walk     192.168.1.5 --slot 1
./bin/dhs consumer acp1 get      192.168.1.5 --slot 1 --path "control.video_gain"
./bin/dhs consumer acp1 set      192.168.1.5 --slot 1 --path "control.video_gain" --value -3.0
./bin/dhs consumer acp1 watch    192.168.1.5 --slot 1

./bin/dhs producer acp1 serve    --tree tree.json --port 2071

./bin/dhs registry serve         --port 8080
```

Full CLI reference per protocol in `internal/<proto>/CLAUDE.md`. Canonical
verbs + flags are locked by ADR-0002.

---

## 5. Cross-compile — release binaries for all OS

The Go toolchain cross-compiles out of the box. `make build-all` runs from
**either** the container **or** Windows and produces every target.

```bash
make build-all
```

Output layout:

```text
dist/
  dhs_linux_amd64/dhs
  dhs_linux_arm64/dhs
  dhs_darwin_amd64/dhs
  dhs_darwin_arm64/dhs
  dhs_windows_amd64/dhs.exe
```

Per-target builds if you only need one:

```bash
make build-linux-amd64
make build-linux-arm64
make build-darwin-amd64
make build-darwin-arm64
make build-windows-amd64
```

Archives for distribution:

```bash
make package           # creates dist/*.tar.gz (linux/darwin) and dist/*.zip (windows)
```

---

## 6. Wireshark verification (optional but recommended)

When touching any wire codec, capture real traffic and compare bytes
against your unit-test expectations.

1. Install Wireshark (see section 1).
2. Copy each plugin's Lua dissector into your Wireshark personal plugins
   directory (`%APPDATA%\Wireshark\plugins\` on Windows,
   `~/.local/lib/wireshark/plugins/` on Linux/macOS):
   - `internal/acp1/wireshark/dhs_acpv1.lua`
   - `internal/acp2/wireshark/dhs_acpv2.lua`
   - `internal/emberplus/wireshark/dhs_emberplus.lua`
   - `internal/osc/wireshark/dhs_osc.lua`
   - `internal/probel-sw08p/wireshark/dhs_probel_sw08p.lua`
3. Restart Wireshark. Dissectors auto-load from the plugins directory; no
   `init.lua` edits are needed.
4. Capture on your device-facing interface, filter by any of the
   `dhs_<proto>` display filters (e.g. `dhs_acpv1`, `dhs_acpv2`,
   `dhs_emberplus`, `dhs_osc`, `dhs_probel_sw08p`).

---

## 7. Release

Release is a tag on `main`. CI cross-compiles, runs `make package`, and
attaches the archives to a GitHub Release.

```bash
git checkout main
git pull
git tag -a v0.1.0 -m "dhs v0.1.0"
git push origin v0.1.0
```

---

## 8. Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| `go: command not found` inside container | PATH not refreshed | Exit VS Code terminal, reopen (`Ctrl+` `` ` ``) |
| `make: command not found` on Windows | `GnuWin32.Make` not installed or PATH not refreshed | Install + reopen shell |
| `discover` finds nothing in container | UDP broadcast NAT'd by Docker | Run `./bin/dhs consumer acp1 discover` from Windows instead |
| `go build` fails with import cycle | Something outside `cmd/` imported `internal/<proto>/consumer` or `internal/<proto>/provider` | Only `cmd/dhs/` may import plugin packages |
| Integration test hangs | Device not reachable | `ping $ACP1_TEST_HOST` first; check firewall for UDP 2071 |
| Post-create script fails | Network inside container | Rebuild container: VS Code → `Dev Containers: Rebuild Container` |
| `winget` says package not found | Old winget / no internet | `winget source update` then retry |

---

## 9. Per-protocol runbooks

Each protocol has its own atomic context and (where applicable) docs:

- [internal/acp1/CLAUDE.md](internal/acp1/CLAUDE.md) — UDP/TCP direct
- [internal/acp2/CLAUDE.md](internal/acp2/CLAUDE.md) — AN2/TCP
- [internal/emberplus/CLAUDE.md](internal/emberplus/CLAUDE.md)
- [internal/probel-sw08p/CLAUDE.md](internal/probel-sw08p/CLAUDE.md)
- [internal/probel-sw02p/CLAUDE.md](internal/probel-sw02p/CLAUDE.md)
- [internal/osc/CLAUDE.md](internal/osc/CLAUDE.md)
- [internal/tsl/CLAUDE.md](internal/tsl/CLAUDE.md)
- [internal/cerebrum-nb/CLAUDE.md](internal/cerebrum-nb/CLAUDE.md)
- [internal/amwa/CLAUDE.md](internal/amwa/CLAUDE.md) — NMOS

---

## 10. What to read next

- [CLAUDE.md](CLAUDE.md) — Go conventions, error hierarchy, scale targets
- [AGENTS.md](AGENTS.md) — cross-repo task patterns, testing rules, invariants
- [docs/adr/](docs/adr/README.md) — Architecture Decision Records (binding)
- [docs/CONNECTOR.md](docs/CONNECTOR.md) — connector contract (collated ADRs)
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — three-layer architecture overview
- [docs/deployment/README.md](docs/deployment/README.md) — cross-compile and firewall rules
