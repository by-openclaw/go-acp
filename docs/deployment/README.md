# Deployment

## Cross-compile

The Go toolchain cross-compiles out of the box. No CGO dependencies.

```bash
# Linux amd64
GOOS=linux GOARCH=amd64 go build -o dist/acp_linux_amd64/acp ./cmd/acp
GOOS=linux GOARCH=amd64 go build -o dist/acp_linux_amd64/acp-srv ./cmd/acp-srv

# Linux arm64
GOOS=linux GOARCH=arm64 go build -o dist/acp_linux_arm64/acp ./cmd/acp
GOOS=linux GOARCH=arm64 go build -o dist/acp_linux_arm64/acp-srv ./cmd/acp-srv

# macOS amd64
GOOS=darwin GOARCH=amd64 go build -o dist/acp_darwin_amd64/acp ./cmd/acp
GOOS=darwin GOARCH=amd64 go build -o dist/acp_darwin_amd64/acp-srv ./cmd/acp-srv

# macOS arm64 (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o dist/acp_darwin_arm64/acp ./cmd/acp
GOOS=darwin GOARCH=arm64 go build -o dist/acp_darwin_arm64/acp-srv ./cmd/acp-srv

# Windows amd64
GOOS=windows GOARCH=amd64 go build -o dist/acp_windows_amd64/acp.exe ./cmd/acp
GOOS=windows GOARCH=amd64 go build -o dist/acp_windows_amd64/acp-srv.exe ./cmd/acp-srv
```

Or use `make build-all` to build all targets at once.

## Port requirements

| Port | Protocol | Transport | Used by |
|------|----------|-----------|---------|
| 2071 | ACP1     | UDP       | discover, get, set, walk, watch |
| 2071 | ACP1     | TCP       | ACP v1.4 TCP direct mode |
| 2072 | ACP2     | TCP       | AN2/TCP (ACP2, future) |

## Firewall rules

### Linux (UFW)

```bash
sudo ufw allow 2071/udp comment "ACP1 UDP"
sudo ufw allow 2071/tcp comment "ACP1 TCP direct"
sudo ufw allow 2072/tcp comment "ACP2 AN2/TCP"
```

### Linux (firewalld)

```bash
sudo firewall-cmd --permanent --add-port=2071/udp
sudo firewall-cmd --permanent --add-port=2071/tcp
sudo firewall-cmd --permanent --add-port=2072/tcp
sudo firewall-cmd --reload
```

### Windows Firewall (PowerShell, run as Administrator)

```powershell
New-NetFirewallRule -DisplayName "ACP1 UDP" -Direction Inbound -Protocol UDP -LocalPort 2071 -Action Allow
New-NetFirewallRule -DisplayName "ACP1 TCP" -Direction Inbound -Protocol TCP -LocalPort 2071 -Action Allow
New-NetFirewallRule -DisplayName "ACP2 AN2/TCP" -Direction Inbound -Protocol TCP -LocalPort 2072 -Action Allow
```

### macOS (pf)

Add to `/etc/pf.conf`:

```
pass in proto udp from any to any port 2071
pass in proto tcp from any to any port 2071
pass in proto tcp from any to any port 2072
```

Then reload:

```bash
sudo pfctl -f /etc/pf.conf
```

---

Copyright (c) 2026 BY-SYSTEMS SRL - www.by-systems.be
