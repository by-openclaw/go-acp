package acp1

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"dhs/internal/dmlib"
	"dhs/internal/export"
	"dhs/internal/protocol"
)

func TestParseCardPath(t *testing.T) {
	cases := []struct {
		in   string
		want dmlib.Fingerprint
		err  bool
	}{
		{
			in: "axon/synapse/RRS18-1601/acp1",
			want: dmlib.Fingerprint{
				Vendor: "axon", Product: "synapse",
				Model: "RRS18", SwRev: "1601", Proto: "acp1",
			},
		},
		{
			// Hyphenated model: split on the LAST '-'.
			in: "lawo/vsm/GIO-12-2000/acp2",
			want: dmlib.Fingerprint{
				Vendor: "lawo", Product: "vsm",
				Model: "GIO-12", SwRev: "2000", Proto: "acp2",
			},
		},
		{in: "too/few/parts", err: true},
		{in: "too/many/parts/here/now", err: true},
		{in: "axon//RRS18-1601/acp1", err: true},
		{in: "axon/synapse/RRS18/acp1", err: true},   // no rev separator
		{in: "axon/synapse/-1601/acp1", err: true},   // empty model
		{in: "axon/synapse/RRS18-/acp1", err: true},  // empty rev
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseCardPath(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("expected error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// fakeDMResolver satisfies dmlib.Resolver with canned answers.
type fakeDMResolver struct {
	resolveErr error
	calledFP   dmlib.Fingerprint
}

func (r *fakeDMResolver) Resolve(fp dmlib.Fingerprint) (*dmlib.Schema, error) {
	r.calledFP = fp
	if r.resolveErr != nil {
		return nil, r.resolveErr
	}
	return &dmlib.Schema{
		Fingerprint: fp,
		Slots: map[int]*export.Snapshot{
			1: {Slots: []export.SlotDump{{Slot: 1, Objects: []protocol.Object{}}}},
		},
	}, nil
}
func (r *fakeDMResolver) LookupAlternate(fp dmlib.Fingerprint) ([]dmlib.Fingerprint, error) {
	return nil, nil
}
func (r *fakeDMResolver) Persist(s *dmlib.Schema) error      { return nil }
func (r *fakeDMResolver) Diff(p, c *dmlib.Schema) dmlib.Diff { return dmlib.Diff{} }

func TestSlotLoad_NoResolverConfigured(t *testing.T) {
	s := newTestServer(t)
	err := s.SlotLoad(context.Background(), 1, "axon/synapse/RRS18-1601/acp1")
	if !errors.Is(err, ErrNoDMLibrary) {
		t.Fatalf("err = %v, want ErrNoDMLibrary", err)
	}
}

func TestSlotLoad_BadCardPath(t *testing.T) {
	s := newTestServer(t)
	s.SetDMLibrary(&fakeDMResolver{})
	err := s.SlotLoad(context.Background(), 1, "totally bogus")
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}

func TestSlotLoad_ResolverMiss(t *testing.T) {
	s := newTestServer(t)
	s.SetDMLibrary(&fakeDMResolver{resolveErr: dmlib.ErrNotFound})
	err := s.SlotLoad(context.Background(), 1, "axon/synapse/RRS18-1601/acp1")
	if !errors.Is(err, dmlib.ErrNotFound) {
		t.Fatalf("err = %v, want dmlib.ErrNotFound", err)
	}
}

func TestSlotLoad_DrivesCascade(t *testing.T) {
	s := newTestServer(t)
	s.SetInsertTiming(InsertTimingFast)
	s.SetDMLibrary(&fakeDMResolver{})
	if err := s.setSlotStatus(1, 0); err != nil {
		t.Fatalf("reset slot: %v", err)
	}

	if err := s.SlotLoad(context.Background(), 1, "axon/synapse/RRS18-1601/acp1"); err != nil {
		t.Fatalf("SlotLoad: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if readSlotStatus(t, s, 1) == 2 /* present */ {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cascade did not reach present after SlotLoad: state = %d", readSlotStatus(t, s, 1))
}

func TestSlotUnload_DrivesExtract(t *testing.T) {
	s := newTestServer(t)
	// fixture starts slot 1 at present.
	s.SlotUnload(1)
	if got := readSlotStatus(t, s, 1); got != 0 {
		t.Fatalf("after SlotUnload: state = %d, want no_card (0)", got)
	}
}

func TestSlotLoad_PassesFingerprintToResolver(t *testing.T) {
	s := newTestServer(t)
	r := &fakeDMResolver{}
	s.SetDMLibrary(r)

	const path = "axon/synapse/RRS18-1601/acp1"
	if err := s.SlotLoad(context.Background(), 1, path); err != nil {
		t.Fatalf("SlotLoad: %v", err)
	}
	want := dmlib.Fingerprint{
		Vendor: "axon", Product: "synapse",
		Model: "RRS18", SwRev: "1601", Proto: "acp1",
	}
	if r.calledFP != want {
		t.Fatalf("resolver called with %+v, want %+v", r.calledFP, want)
	}
}
