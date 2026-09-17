package provider

import (
	"errors"
	"testing"
)

// The Node's certificate must cover every name a peer can reach it
// by: the advertised name when it is a name (an IP is not a DNS SAN
// under BCP-003-01), plus the machine's own name and its .local form.
func TestTLSIdentities(t *testing.T) {
	useHostname(t, "node-host", nil)

	for name, tc := range map[string]struct {
		advertise string
		want      []string
	}{
		"an advertised name with a port": {
			"node.example:8080", []string{"node.example", "node-host", "node-host.local"},
		},
		"a bare advertised name": {
			"node.example", []string{"node.example", "node-host", "node-host.local"},
		},
		"an advertised IP is not a DNS name": {
			"10.6.239.113:8080", []string{"node-host", "node-host.local"},
		},
		"no advertise host at all": {
			"", []string{"node-host", "node-host.local"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := tlsIdentities(tc.advertise)
			if len(got) != len(tc.want) {
				t.Fatalf("= %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("= %v, want %v", got, tc.want)
					break
				}
			}
		})
	}

	// With no name to be had anywhere, the certificate still needs one
	// subject — a nameless certificate is not installable.
	useHostname(t, "", errors.New("no hostname"))
	if got := tlsIdentities(""); len(got) != 1 || got[0] != "dhs-node" {
		t.Errorf("= %v, want the fallback identity", got)
	}
}

// A Node with no auth and no provisioned trust hands the registration
// client neither — a nil token source or root pool would be worse
// than none, and the client must not be handed one.
func TestAttachHelpersAreNoOpsWithoutTheirSubsystem(t *testing.T) {
	s := &IS04NodeServer{}
	rc := NewRegistrationClient(newLogTap().logger(), "http://registry.invalid:1", "v1.3", validBundle())

	s.attachAuthToken(rc)
	s.attachTLSTrust(rc)
	if rc.tokenSource != nil {
		t.Error("a Node with no auth must not install a token source")
	}
	// SetTLSRoots installs a client transport; with no provisioned
	// trust it is never called, so the client keeps its default.
	if rc.http != nil && rc.http.Transport != nil {
		t.Error("a Node with no provisioned trust must not install a TLS transport")
	}
}

// With no Connection API there is no activation to propagate, so the
// hook is not installed at all.
func TestWireResourceChangedWithoutAConnectionAPI(t *testing.T) {
	s := &IS04NodeServer{}
	s.wireResourceChanged(nil) // must not panic on the nil connection
}
