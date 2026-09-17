package provider

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
)

// fakeDNSZone answers every query it receives with the same canned
// announce bytes (response ID fixed up per query). resolveOnce asks
// for BOTH the modern and legacy service names, so the server loops.
func fakeDNSZone(t *testing.T, response []byte) (addr string, stop func()) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			// A real zone holds records for ONE service name. Answer
			// modern-name questions (PTR browse + per-instance chase)
			// with the canned records; everything else — notably the
			// legacy `_nmos-registration._tcp` pass — gets an empty
			// response (query echoed, QR bit set, zero answers), the
			// way a DNS server answers a name it has no records for.
			// Answering everything with modern records would hand the
			// legacy pass a chase target and mint a twin candidate
			// under the legacy FullName — a zone shape that does not
			// exist in the field.
			answerable := false
			if req, err := dnssdcodec.Decode(buf[:n]); err == nil && len(req.Questions) == 1 {
				q := req.Questions[0].Name
				answerable = strings.HasPrefix(q, dnssdcodec.ServiceRegister+".") ||
					strings.Contains(q, "."+dnssdcodec.ServiceRegister+".")
			}
			var out []byte
			if answerable {
				out = make([]byte, len(response))
				copy(out, response)
			} else {
				out = make([]byte, n)
				copy(out, buf[:n])
				out[2] |= 0x80 // QR: this is a response — with no answers
			}
			out[0] = buf[0]
			out[1] = buf[1]
			if _, err := conn.WriteToUDP(out, src); err != nil {
				return
			}
		}
	}()
	return conn.LocalAddr().String(), func() { _ = conn.Close(); wg.Wait() }
}

func unicastZoneInstance(t *testing.T, pri string) []byte {
	t.Helper()
	inst := dnssdcodec.Instance{
		Name:    "zone-registry",
		Service: dnssdcodec.ServiceRegister,
		Domain:  "test.arpa",
		Host:    "reg.test.arpa",
		Port:    8235,
		IPv4:    []net.IP{net.IPv4(10, 100, 0, 42).To4()},
		TXT: map[string]string{
			dnssdcodec.TXTKeyAPIProto: "http",
			dnssdcodec.TXTKeyAPIVer:   "v1.2,v1.3",
			dnssdcodec.TXTKeyAPIAuth:  "false",
			dnssdcodec.TXTKeyPriority: pri,
		},
	}
	wire, err := dnssdcodec.EncodeAnnounce(inst, true)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return wire
}

// The watcher resolves a zone, builds the same candidate the mDNS
// watcher would, and serves it through the same Best() contract.
func TestUnicastRegistryWatcherResolvesAndSelects(t *testing.T) {
	addr, stop := fakeDNSZone(t, unicastZoneInstance(t, "10"))
	defer stop()

	w := NewUnicastRegistryWatcher(nil, addr, "test.arpa", "v1.3")
	w.resolveOnce(context.Background())

	cand, ok := w.Best()
	if !ok {
		t.Fatal("Best: no candidate after resolve")
	}
	if cand.URL != "http://10.100.0.42:8235" {
		t.Errorf("URL: %q", cand.URL)
	}
	if cand.Priority != 10 {
		t.Errorf("Priority: %d", cand.Priority)
	}
	if cand.APIVer != "v1.3" {
		t.Errorf("APIVer: %q (highest mutual of v1.2,v1.3 vs v1.3)", cand.APIVer)
	}
}

// Disqualify hides the candidate — the RegistrationClient's failover
// contract — and a re-resolve (the zone still publishing it) restores
// it, mirroring the mDNS re-announcement rule.
func TestUnicastRegistryWatcherDisqualifyAndRecover(t *testing.T) {
	addr, stop := fakeDNSZone(t, unicastZoneInstance(t, "0"))
	defer stop()

	w := NewUnicastRegistryWatcher(nil, addr, "test.arpa", "v1.3")
	w.resolveOnce(context.Background())
	cand, ok := w.Best()
	if !ok {
		t.Fatal("no candidate")
	}
	w.Disqualify(cand.FullName)
	if c2, ok := w.Best(); ok {
		w.mu.Lock()
		keys := make([]string, 0, len(w.byFull))
		for k := range w.byFull {
			keys = append(keys, k)
		}
		dq := make([]string, 0, len(w.disqualified))
		for k := range w.disqualified {
			dq = append(dq, k)
		}
		w.mu.Unlock()
		t.Fatalf("disqualified candidate must not be Best:\n got %+v\n disqualified %q\n byFull keys %q", c2, dq, keys)
	}
	w.resolveOnce(context.Background())
	if _, ok := w.Best(); !ok {
		t.Fatal("re-resolve while still published must clear the disqualification")
	}
}
