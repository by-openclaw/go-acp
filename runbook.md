# runbook.md — acp developer runbook

Step-by-step guide to set up the dev environment, build, test, and release.

Two supported workflows:

- **Devcontainer (recommended)** — everything in a Linux container, one-time
  install on Windows. Reproducible, no host pollution.
- **Native Windows** — Go + Node installed directly on Windows. Required for
  talking to real ACP devices over UDP broadcast (devcontainers NAT UDP).

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
go version        # go1.22.x
node --version    # v20.x or v22.x
git --version
golangci-lint --version
```

### 1b. Native Windows workflow

```powershell
winget install --id GoLang.Go -e
winget install --id OpenJS.NodeJS.LTS -e
winget install --id Git.Git -e
winget install --id golangci-lint.golangci-lint -e
winget install --id GnuWin32.Make -e              # for `make` targets
winget install --id WiresharkFoundation.Wireshark -e   # for ACP capture
```

**Open a new shell** after installing so PATH refreshes. Verify:

```powershell
go version
node --version
make --version
```

Then clone/open the repo:

```powershell
cd C:\Users\BY-SYSTEMSSRLBoujraf\Downloads\acp
go mod tidy
```

---

## 2. Build

All targets work in both environments.

| Target              | Command                                | Produces                         |
|---------------------|----------------------------------------|----------------------------------|
| Both binaries       | `make build`                           | `bin/acp`, `bin/acp-srv`         |
| CLI only            | `make build-cli`                       | `bin/acp`                        |
| Server only         | `make build-srv`                       | `bin/acp-srv`                    |
| Plain `go build`    | `go build ./...`                       | cached, no output files          |

Without `make`:

```bash
go build -o bin/acp       ./cmd/acp
go build -o bin/acp-srv   ./cmd/acp-srv
```

On Windows the binaries are `bin\acp.exe` and `bin\acp-srv.exe`.

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

### 4a. CLI

After `make build`:

```bash
./bin/acp discover --protocol acp1
./bin/acp connect  192.168.1.5 --protocol acp1
./bin/acp walk     192.168.1.5 --protocol acp1 --slot 1
./bin/acp get      192.168.1.5 --protocol acp1 --slot 1 --group control --label "Video Gain"
./bin/acp set      192.168.1.5 --protocol acp1 --slot 1 --group control --label "Video Gain" --value -3.0
./bin/acp watch    192.168.1.5 --protocol acp1 --slot 1
```

Full CLI reference in [CLAUDE.md](CLAUDE.md).

### 4b. Server

```bash
./bin/acp-srv --addr :8080 --log-level info
```

Then `acp-ui` talks to it at `http://localhost:8080`. In a devcontainer,
port 8080 is forwarded to the Windows host automatically.

---

## 5. Cross-compile — release binaries for all OS

The Go toolchain cross-compiles out of the box. `make build-all` runs from
**either** the container **or** Windows and produces every target.

```bash
make build-all
```

Output layout:

```
dist/
  acp_linux_amd64/{acp, acp-srv}
  acp_linux_arm64/{acp, acp-srv}
  acp_darwin_amd64/{acp, acp-srv}
  acp_darwin_arm64/{acp, acp-srv}
  acp_windows_amd64/{acp.exe, acp-srv.exe}
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

When touching the ACP1 codec, capture real traffic and compare bytes
against your unit-test expectations.

1. Install Wireshark (see section 1).
2. Copy the Lua dissectors from `assets/` to your Wireshark init path:
   - Windows: `%APPDATA%\Wireshark\init.lua`
3. Append:
   ```lua
   local axon_dir = "C:/Users/BY-SYSTEMSSRLBoujraf/Downloads/acp/assets/"
   dofile(axon_dir .. "dissector_acpv1.lua")
   dofile(axon_dir .. "dissector_acp2.lua")
   ```
4. Capture on your device-facing interface, filter `udp.port == 2071 || tcp.port == 2071 || tcp.port == 2072`.

---

## 7. Release

Release is a tag on `main`. CI cross-compiles, runs `make package`, and
attaches the archives to a GitHub Release.

```bash
git checkout main
git pull
git tag -a v0.1.0 -m "acp v0.1.0"
git push origin v0.1.0
```

---

## 8. Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `go: command not found` inside container | PATH not refreshed | Exit VS Code terminal, reopen (`Ctrl+` `` ` ``) |
| `make: command not found` on Windows | `GnuWin32.Make` not installed or PATH not refreshed | Install + reopen shell |
| `discover` finds nothing in container | UDP broadcast NAT'd by Docker | Run `./bin/acp discover` from Windows instead |
| `go build` fails with import cycle | Something outside `cmd/` imported `internal/protocol/acp1` | Only `cmd/` may import plugin packages |
| Integration test hangs | Device not reachable | `ping $ACP1_TEST_HOST` first; check firewall for UDP 2071 |
| Post-create script fails | Network inside container | Rebuild container: VS Code → `Dev Containers: Rebuild Container` |
| `winget` says package not found | Old winget / no internet | `winget source update` then retry |

---

## 9. Per-protocol runbooks

Each protocol has its own README with CLI examples, integration test
instructions, and known limitations:

- [ACP1](internal/acp1/docs/README.md) — implemented, UDP/TCP direct
- [ACP2](internal/acp2/docs/README.md) — not yet implemented, AN2/TCP

---

## 10. What to read next

- [CLAUDE.md](CLAUDE.md) — protocol reference, architecture rules, wire formats
- [agents.md](agents.md) — cross-repo task patterns, testing rules, invariants
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — three-layer architecture overview
- [docs/deployment/README.md](docs/deployment/README.md) — cross-compile and firewall rules
- `docs/protocols/AXON-ACP_v1_4.pdf` — authoritative ACP1 spec
- `internal/acp2/assets/acp2_protocol.pdf` — authoritative ACP2 spec
- `docs/protocols/an2_protocol.pdf` — AN2 transport spec
