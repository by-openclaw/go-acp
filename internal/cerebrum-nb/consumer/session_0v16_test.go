package cerebrumnb

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/cerebrum-nb/codec"
)

// ----------------------------------------------------------------------
// 0v16 §4.5 DEVICE_CONFIGURATION — Session.DeviceConfig
// ----------------------------------------------------------------------

// deviceConfigResultFor builds the §4.5 reply envelope echoing the request's
// MTID and wrapping a single <RESULT VALUE="…"/>.
func deviceConfigResultFor(frame []byte, value string) []byte {
	return []byte(`<DEVICE_CONFIGURATION MTID="` + mtidOf(frame) +
		`" TYPE="ADD" IP_ADDRESS="10.0.0.9" DEVICE_TYPE="GENERIC"><RESULT VALUE="` +
		value + `"/></DEVICE_CONFIGURATION>`)
}

func TestDeviceConfig_NilGuard(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) { _, _ = fc.readClientFrame() })
	_, sess := dialFake(t, fs)
	if _, err := sess.DeviceConfig(context.Background(), nil); err == nil ||
		!strings.Contains(err.Error(), "nil configuration") {
		t.Fatalf("want nil-config guard, got %v", err)
	}
}

func TestDeviceConfig_Accepted_AllTypes(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		drainClient(fc, func(frame []byte) ([]byte, bool) {
			return deviceConfigResultFor(frame, "ACCEPTED"), true
		})
	})
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()

	cases := []struct {
		name string
		dc   *codec.DeviceConfiguration
	}{
		{"generic-add", &codec.DeviceConfiguration{
			Type: codec.DeviceConfigAdd, IPAddress: "10.0.0.9", DeviceType: codec.ConfigDeviceGeneric,
			Generic: &codec.GenericConfig{Device: "DRV", Name: "G1",
				Protocol: &codec.ProtocolConfiguration{ConnectionType: codec.ConnTCP, PortNumber: "9000"}},
		}},
		{"panel-modify", &codec.DeviceConfiguration{
			Type: codec.DeviceConfigModify, IPAddress: "10.0.0.9", DeviceName: "P1", DeviceType: codec.ConfigDevicePanel,
			Panel: &codec.PanelConfig{Device: "DRV", Name: "P1", CPF: "cpf", PanelType: "T"},
		}},
		{"router-add", &codec.DeviceConfiguration{
			Type: codec.DeviceConfigAdd, IPAddress: "10.0.0.9", DeviceType: codec.ConfigDeviceRouter,
			Router: &codec.RouterConfig{Device: "DRV", Name: "R1", Type: "5",
				Serial: &codec.SerialConfig{Baud: "9600"},
				Router: &codec.RouterParams{MaxLevel: "4", MaxSource: "1024", MaxDest: "1024"}},
		}},
		{"snmp-add", &codec.DeviceConfiguration{
			Type: codec.DeviceConfigAdd, IPAddress: "10.0.0.9", DeviceType: codec.ConfigDeviceSNMP,
			SNMP: &codec.SNMPConfig{Name: "S1", Port: "161"},
		}},
		{"remove", &codec.DeviceConfiguration{
			Type: codec.DeviceConfigRemove, IPAddress: "10.0.0.9", DeviceType: codec.ConfigDeviceGeneric,
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := sess.DeviceConfig(ctx, c.dc)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if !res.Accepted || res.Value != "ACCEPTED" {
				t.Fatalf("%s: result = %+v", c.name, res)
			}
		})
	}
}

func TestDeviceConfig_Failed_FiresCompliance(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		drainClient(fc, func(frame []byte) ([]byte, bool) {
			return deviceConfigResultFor(frame, "FAILED"), true
		})
	})
	p, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	res, err := sess.DeviceConfig(ctx, &codec.DeviceConfiguration{
		Type: codec.DeviceConfigAdd, IPAddress: "10.0.0.9", DeviceType: codec.ConfigDeviceSNMP,
		SNMP: &codec.SNMPConfig{Name: "S1", Port: "161"},
	})
	if err != nil {
		t.Fatalf("DeviceConfig: %v", err)
	}
	if res.Accepted {
		t.Fatal("want not accepted")
	}
	if p.Compliance().Counts()["cerebrum_device_config_failed"] != 1 {
		t.Fatalf("device_config_failed compliance not recorded: %+v", p.Compliance().Counts())
	}
}

func TestDeviceConfig_EmptyResultBody(t *testing.T) {
	// DEVICE_CONFIGURATION reply without a <RESULT> child → DeviceConfig is
	// non-nil (parsed) but Value empty / not accepted; the method still
	// returns it. To hit the explicit "empty RESULT body" arm we need
	// f.DeviceConfig == nil, which the decoder never produces for a
	// device_configuration root — so instead assert the no-RESULT reply is
	// surfaced as a not-accepted result (the realistic shape).
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		drainClient(fc, func(frame []byte) ([]byte, bool) {
			return []byte(`<DEVICE_CONFIGURATION MTID="` + mtidOf(frame) + `" TYPE="REMOVE"/>`), true
		})
	})
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	res, err := sess.DeviceConfig(ctx, &codec.DeviceConfiguration{
		Type: codec.DeviceConfigRemove, IPAddress: "10.0.0.9", DeviceType: codec.ConfigDeviceGeneric,
	})
	if err != nil {
		t.Fatalf("DeviceConfig: %v", err)
	}
	if res.Accepted {
		t.Fatalf("no-RESULT reply should not be accepted: %+v", res)
	}
}

func TestDeviceConfig_Nack(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		drainClient(fc, func(frame []byte) ([]byte, bool) {
			return nackFor(frame, "NOT_LOGGED_IN", 6), true
		})
	})
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	_, err := sess.DeviceConfig(ctx, &codec.DeviceConfiguration{
		Type: codec.DeviceConfigAdd, IPAddress: "10.0.0.9", DeviceType: codec.ConfigDeviceSNMP,
		SNMP: &codec.SNMPConfig{Name: "S1", Port: "161"},
	})
	var ne *codec.NackError
	if !errors.As(err, &ne) {
		t.Fatalf("want NackError, got %v", err)
	}
}

func TestDeviceConfig_Busy(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		drainClient(fc, func(frame []byte) ([]byte, bool) {
			return []byte(`<BUSY MTID="` + mtidOf(frame) + `"/>`), true
		})
	})
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	_, err := sess.DeviceConfig(ctx, &codec.DeviceConfiguration{
		Type: codec.DeviceConfigAdd, IPAddress: "10.0.0.9", DeviceType: codec.ConfigDeviceSNMP,
		SNMP: &codec.SNMPConfig{Name: "S1", Port: "161"},
	})
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("want busy error, got %v", err)
	}
}

func TestDeviceConfig_UnexpectedReply(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		drainClient(fc, func(frame []byte) ([]byte, bool) {
			return []byte(`<POLL_REPLY MTID="` + mtidOf(frame) + `"/>`), true
		})
	})
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	_, err := sess.DeviceConfig(ctx, &codec.DeviceConfiguration{
		Type: codec.DeviceConfigAdd, IPAddress: "10.0.0.9", DeviceType: codec.ConfigDeviceSNMP,
		SNMP: &codec.SNMPConfig{Name: "S1", Port: "161"},
	})
	if err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("want unexpected reply, got %v", err)
	}
}

// TestDeviceConfig_RoundTripError drives the roundTrip-error arm (write fails
// because the server closed the connection).
func TestDeviceConfig_RoundTripError(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) { _ = fc.c.Close() })
	_, sess := dialFake(t, fs)
	waitFor(t, time.Second, func() bool {
		c, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, err := sess.DeviceConfig(c, &codec.DeviceConfiguration{
			Type: codec.DeviceConfigRemove, IPAddress: "10.0.0.9", DeviceType: codec.ConfigDeviceGeneric,
		})
		return err != nil
	}, "device-config round-trip error after server close")
}

// ----------------------------------------------------------------------
// 0v16 §1.4 CONTINUE / §1.6 WILDCARD_COMPLETE flow-control dispatch
// ----------------------------------------------------------------------

func TestDispatch_Continue_FiresComplianceAndFansOut(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		frame, err := fc.readClientFrame() // the SUBSCRIBE
		if err != nil {
			return
		}
		_ = fc.writeText(ackFor(frame))
		// Push a free-standing CONTINUE flow-control frame.
		_ = fc.writeText([]byte(`<CONTINUE/>`))
		_, _ = fc.readClientFrame() // hold open
	})
	p, sess := dialFake(t, fs)

	var continueN atomic.Int32
	sess.OnEvent(codec.KindContinue, func(*codec.Frame) { continueN.Add(1) })

	ctx, cancel := ctx2s(t)
	defer cancel()
	if err := sess.Subscribe(ctx, []codec.SubItem{&codec.DeviceChange{Type: "LIST"}}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		return continueN.Load() == 1 && p.Compliance().Counts()["cerebrum_continue_received"] == 1
	}, "CONTINUE fan-out + compliance")
}

func TestDispatch_WildcardComplete_FansOut(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		frame, err := fc.readClientFrame() // the SUBSCRIBE
		if err != nil {
			return
		}
		_ = fc.writeText(ackFor(frame))
		// Push the end-of-snapshot marker carrying the SUBSCRIBE mtid.
		_ = fc.writeText([]byte(`<WILDCARD_COMPLETE MTID="` + mtidOf(frame) + `"/>`))
		_, _ = fc.readClientFrame() // hold open
	})
	_, sess := dialFake(t, fs)

	var doneN atomic.Int32
	sess.OnEvent(codec.KindWildcardComplete, func(*codec.Frame) { doneN.Add(1) })

	ctx, cancel := ctx2s(t)
	defer cancel()
	if err := sess.Subscribe(ctx, []codec.SubItem{&codec.DeviceChange{Type: "LIST"}}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool { return doneN.Load() == 1 }, "WILDCARD_COMPLETE snapshot marker")
}

// ----------------------------------------------------------------------
// 0v16 §5.5 datastore obtain DATA body surfaced through ObtainDatastore
// ----------------------------------------------------------------------

func TestObtainDatastore_DataBody(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		frame, err := fc.readClientFrame() // the OBTAIN
		if err != nil {
			return
		}
		_ = fc.writeText(ackFor(frame))
		// Obtain reply with a <DATA> body (§5.5.1).
		_ = fc.writeText([]byte(`<DATASTORE_CHANGE TYPE="FILE" AVAILABLE="1"><DATA><ENTRY KEY="a" VALUE="1"/></DATA></DATASTORE_CHANGE>`))
		_, _ = fc.readClientFrame() // hold open
	})
	_, sess := dialFake(t, fs)

	got := make(chan *codec.DatastoreChange, 1)
	sess.OnEvent(codec.KindDatastoreChange, func(f *codec.Frame) {
		if f.Datastore != nil {
			select {
			case got <- f.Datastore:
			default:
			}
		}
	})
	ctx, cancel := ctx2s(t)
	defer cancel()
	if err := sess.ObtainDatastore(ctx, "path/to/store"); err != nil {
		t.Fatalf("ObtainDatastore: %v", err)
	}
	select {
	case ds := <-got:
		if !ds.Available {
			t.Fatal("want available")
		}
		// The decoder case-folds child element names, so <ENTRY> renders
		// back as <entry …/>. Assert on the lowercased form + the values.
		if !strings.Contains(strings.ToLower(ds.Data), "entry") || !strings.Contains(ds.Data, `value="1"`) {
			t.Fatalf("DATA body not surfaced: %q", ds.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no datastore reply with DATA body")
	}
}
