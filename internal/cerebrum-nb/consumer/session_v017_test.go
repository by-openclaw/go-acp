package cerebrumnb

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dhs/internal/cerebrum-nb/codec"
)

// Northbound v0.17 §4.2.3-§4.2.5: RM_SRCE_ADD / RM_DEST_ADD /
// RM_SRCE_DELETE / RM_DEST_DELETE and their <ROUTING><RESULT/> reply, plus
// the FEDERATION attribute on tags.

// resultFor builds the §4.2.5 reply echoing the request's TYPE and
// FEDERATION, with the given VALUE.
func resultFor(frame []byte, value string) []byte {
	return []byte(`<ROUTING MTID="` + mtidOf(frame) + `" TYPE="` + attrOf(frame, "TYPE") + `" FEDERATION="` + attrOf(frame, "FEDERATION") + `"><RESULT VALUE="` + value + `"/></ROUTING>`)
}

func TestRMAdd_Result(t *testing.T) {
	var sent []byte
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte { sent = f; return resultFor(f, "ACCEPTED") }))
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	res, err := sess.RMAdd(ctx, RMSources, 4, false, false)
	if err != nil {
		t.Fatalf("RMAdd: %v", err)
	}
	if !res.Accepted || res.Value != "ACCEPTED" || res.Type != "RM_SRCE_ADD" || res.Federation != "0" {
		t.Fatalf("result: %+v", res)
	}
	for _, attr := range []string{`TYPE="RM_SRCE_ADD"`, `COUNT="4"`, `VIRTUAL="0"`, `FEDERATION="0"`} {
		if !strings.Contains(string(sent), attr) {
			t.Errorf("sent frame lacks %s: %s", attr, sent)
		}
	}
	res, err = sess.RMAdd(ctx, RMDestinations, 2, true, true)
	if err != nil || res.Type != "RM_DEST_ADD" || res.Federation != "1" {
		t.Fatalf("RMAdd dest virtual federation: %+v, %v", res, err)
	}
	if !strings.Contains(string(sent), `VIRTUAL="1"`) || !strings.Contains(string(sent), `FEDERATION="1"`) {
		t.Fatalf("sent frame: %s", sent)
	}
}

// Measured on 2.9.3: the RESULT comes first with no MTID, then the ACK.
func TestDeviceConfig_ResultWithoutMTIDBeforeTheAck(t *testing.T) {
	for _, tc := range []struct {
		value string
		ok    bool
	}{{"ACCEPTED", true}, {"FAILED", false}} {
		fs := newFakeCerebrum(t, func(fc *fakeConn) {
			drainClient(fc, func(frame []byte) ([]byte, bool) {
				_ = fc.writeText([]byte(`<DEVICE_CONFIGURATION TYPE="ADD" IP_ADDRESS="10.0.0.9" DEVICE_TYPE="SNMP"><RESULT VALUE="` + tc.value + `"/></DEVICE_CONFIGURATION>`))
				return ackFor(frame), true
			})
		})
		p, sess := dialFake(t, fs)
		ctx, cancel := ctx2s(t)
		res, err := sess.DeviceConfig(ctx, &codec.DeviceConfiguration{Type: codec.DeviceConfigAdd, IPAddress: "10.0.0.9", DeviceType: codec.ConfigDeviceSNMP})
		cancel()
		if err != nil || res.Accepted != tc.ok {
			t.Fatalf("%s: %+v, %v", tc.value, res, err)
		}
		if p.Compliance().Snapshot()["cerebrum_result_without_mtid"] != 1 {
			t.Errorf("%s: the deviation was not counted", tc.value)
		}
	}
}

func TestDeviceConfig_AckWithNoResultIsAnError(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte { return ackFor(f) }))
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	if _, err := sess.DeviceConfig(ctx, &codec.DeviceConfiguration{Type: codec.DeviceConfigRemove, IPAddress: "10.0.0.9", DeviceType: codec.ConfigDeviceSNMP}); err == nil {
		t.Fatal("an ACK with no RESULT passed as a success")
	}
}

func TestRMAdd_FailedResultWithoutMTID(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		drainClient(fc, func(frame []byte) ([]byte, bool) {
			_ = fc.writeText([]byte(`<ROUTING TYPE="RM_SRCE_ADD" FEDERATION="0"><RESULT VALUE="FAILED"/></ROUTING>`))
			return ackFor(frame), true
		})
	})
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	res, err := sess.RMAdd(ctx, RMSources, 1, false, false)
	if err != nil || res.Accepted {
		t.Fatalf("a FAILED result read as accepted: %+v, %v", res, err)
	}
}

func TestRMAdd_AckOnlyServer(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte { return ackFor(f) }))
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	res, err := sess.RMAdd(ctx, RMSources, 1, false, false)
	if err != nil || !res.Accepted || res.Value != "ACCEPTED" {
		t.Fatalf("ack-only server: %+v, %v", res, err)
	}
}

func TestRMAdd_CountOutOfRange(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte { return ackFor(f) }))
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	for _, n := range []int{0, 1001} {
		if _, err := sess.RMAdd(ctx, RMSources, n, false, false); err == nil || !strings.Contains(err.Error(), "1-1000") {
			t.Errorf("count %d: want range error, got %v", n, err)
		}
	}
}

func TestRMDelete_FailedRecordsCompliance(t *testing.T) {
	var sent []byte
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte { sent = f; return resultFor(f, "FAILED") }))
	p, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	res, err := sess.RMDelete(ctx, RMSources, []string{"21", "22", "23"}, false)
	if err != nil {
		t.Fatalf("RMDelete: %v", err)
	}
	if res.Accepted || res.Value != "FAILED" {
		t.Fatalf("result: %+v", res)
	}
	if !strings.Contains(string(sent), `TYPE="RM_SRCE_DELETE" SRCE_ID="21,22,23" FEDERATION="0"`) {
		t.Fatalf("sent frame: %s", sent)
	}
	if p.Compliance().Snapshot()["cerebrum_rm_failed"] != 1 {
		t.Fatalf("cerebrum_rm_failed not recorded: %+v", p.Compliance().Snapshot())
	}
	if _, err := sess.RMDelete(ctx, RMDestinations, []string{"5"}, true); err != nil {
		t.Fatalf("RMDelete dest: %v", err)
	}
	if !strings.Contains(string(sent), `TYPE="RM_DEST_DELETE" DEST_ID="5" FEDERATION="1"`) {
		t.Fatalf("sent frame: %s", sent)
	}
}

func TestRMDelete_NoID(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte { return ackFor(f) }))
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	if _, err := sess.RMDelete(ctx, RMSources, nil, false); err == nil || !strings.Contains(err.Error(), "no id") {
		t.Fatalf("want no-id error, got %v", err)
	}
}

func TestRM_Nack_Busy_Unexpected(t *testing.T) {
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte {
		return nackFor(f, "ONE_OR_MORE_ACTIONS_INVALID", 8)
	}))
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	_, err := sess.RMAdd(ctx, RMSources, 1, false, false)
	var ne *codec.NackError
	if !errors.As(err, &ne) {
		t.Fatalf("want NackError, got %v", err)
	}

	fs = newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte { return []byte(`<BUSY MTID="` + mtidOf(f) + `"/>`) }))
	_, sess = dialFake(t, fs)
	// A BUSY with no CONTINUE waits as long as the caller lets it.
	busyCtx, busyCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer busyCancel()
	if _, err := sess.RMDelete(busyCtx, RMSources, []string{"1"}, false); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("want busy error, got %v", err)
	}

	fs = newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte { return []byte(`<POLL_REPLY MTID="` + mtidOf(f) + `"/>`) }))
	_, sess = dialFake(t, fs)
	if _, err := sess.RMAdd(ctx, RMSources, 1, false, false); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("want unexpected-reply error, got %v", err)
	}
}

func TestSetTags_Federation(t *testing.T) {
	var sent []byte
	fs := newFakeCerebrum(t, actionFakeReplying(func(f []byte) []byte { sent = f; return ackFor(f) }))
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	tgt := DefaultRouteTarget()
	if err := sess.SetTags(ctx, "RM_SRCE_TAGS", tgt, "1", "", "1", "Tag1", true); err != nil {
		t.Fatalf("SetTags federation: %v", err)
	}
	if !strings.Contains(string(sent), `TAGS="Tag1" FEDERATION="1"`) {
		t.Fatalf("sent frame: %s", sent)
	}
	if err := sess.SetTags(ctx, "RM_SRCE_TAGS", tgt, "1", "", "1", "Tag1", false); err != nil {
		t.Fatalf("SetTags local: %v", err)
	}
	if strings.Contains(string(sent), "FEDERATION") {
		t.Fatalf("local tags must not carry FEDERATION: %s", sent)
	}
}
