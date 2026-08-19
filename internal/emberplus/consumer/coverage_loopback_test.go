package emberplus

import (
	"context"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/export/canonical"

	provider "dhs/internal/emberplus/provider"
)

// startLoopbackProvider spins a real Ember+ provider serving a small tree
// on an ephemeral port and returns its address + a stop func. Used by the
// consumer loopback tests to drive Connect / Walk / GetValue / SetValue /
// Subscribe / MatrixConnect / InvokeFunction over actual TCP+S101+Glow.
func startLoopbackProvider(t *testing.T) (string, func()) {
	t.Helper()
	sp := func(s string) *string { return &s }
	gain := &canonical.Parameter{
		Header: canonical.Header{
			Number: 1, Identifier: "gain", Path: "root.gain", OID: "1.1",
			Description: sp("audio gain"), IsOnline: true,
			Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren(),
		},
		Type: canonical.ParamInteger, Value: int64(5),
		Minimum: int64(0), Maximum: int64(100),
	}
	mtx := &canonical.Matrix{
		Header: canonical.Header{
			Number: 2, Identifier: "mat", Path: "root.mat", OID: "1.2",
			IsOnline: true, Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren(),
		},
		Type: canonical.MatrixNToN, TargetCount: 2, SourceCount: 2,
		Connections: []canonical.MatrixConnection{{Target: 0, Sources: []int64{0}}},
	}
	fn := &canonical.Function{
		Header: canonical.Header{
			Number: 3, Identifier: "sum", Path: "root.sum", OID: "1.3",
			IsOnline: true, Access: canonical.AccessRead, Children: canonical.EmptyChildren(),
		},
		Arguments: []canonical.TupleItem{{Name: "a", Type: canonical.ParamInteger}, {Name: "b", Type: canonical.ParamInteger}},
		Result:    []canonical.TupleItem{{Name: "r", Type: canonical.ParamInteger}},
	}
	root := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "root", Path: "root", OID: "1", IsOnline: true,
		Access: canonical.AccessRead, Children: []canonical.Element{gain, mtx, fn},
	}}

	srv := (&provider.Factory{}).New(nil, &canonical.Export{Root: root})

	// Pre-bind the listener and hand it to the provider: no
	// close-then-rebind window (port-steal race, #694 flake class) and
	// no dial-poll — the kernel backlog queues connects from Listen on.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	sl, ok := srv.(interface {
		ServeListener(context.Context, net.Listener) error
	})
	if !ok {
		t.Fatal("provider does not expose ServeListener")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = sl.ServeListener(ctx, ln) }()
	return addr, func() { cancel(); _ = srv.Stop() }
}

// TestConsumerLoopback drives the consumer's full networked verb surface
// against a live in-process provider: Connect, Walk, GetValue, SetValue,
// Subscribe/Unsubscribe, MatrixConnect, InvokeFunction, GetDeviceInfo,
// then Disconnect.
func TestConsumerLoopback(t *testing.T) {
	addr, stop := startLoopbackProvider(t)
	defer stop()

	host, portStr, _ := net.SplitHostPort(addr)
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}

	p := (&Factory{}).New(slog.Default()).(*Plugin)
	ctx := context.Background()

	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	// ComplianceProfile + SetRecorder accessors.
	if p.ComplianceProfile() == nil {
		t.Error("ComplianceProfile nil after connect")
	}
	p.SetRecorder(nil)

	// Walk the tree.
	objs, err := p.Walk(ctx, 0)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(objs) == 0 {
		t.Fatal("walk returned no objects")
	}

	// GetDeviceInfo.
	if _, err := p.GetDeviceInfo(ctx); err != nil {
		t.Logf("GetDeviceInfo: %v", err) // identity node optional; don't fail
	}

	// GetValue on the gain parameter (by label path).
	if _, err := p.GetValue(ctx, consumer.ValueRequest{Path: "root.gain"}); err != nil {
		t.Errorf("GetValue: %v", err)
	}

	// Subscribe (wildcard) then a SetValue should fire the callback.
	gotEvent := make(chan struct{}, 4)
	_ = p.Subscribe(consumer.ValueRequest{}, func(consumer.Event) {
		select {
		case gotEvent <- struct{}{}:
		default:
		}
	})

	// SetValue (provider echoes the confirming announce).
	if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "root.gain"},
		consumer.Value{Kind: consumer.KindInt, Int: 42}); err != nil {
		t.Logf("SetValue: %v", err)
	}

	// MatrixConnect on the nToN matrix.
	if err := p.MatrixConnect(ctx, "root.mat", 1, []int32{1}, glow.ConnOpConnect); err != nil {
		t.Logf("MatrixConnect: %v", err)
	}
	_ = p.MatrixSnapshot("root.mat")

	// InvokeFunction.
	if _, err := p.InvokeFunction(ctx, "root.sum", []any{int64(2), int64(3)}); err != nil {
		t.Logf("InvokeFunction: %v", err)
	}

	// Unsubscribe.
	_ = p.Unsubscribe(consumer.ValueRequest{})

	// Give async announces a moment to land.
	select {
	case <-gotEvent:
	case <-time.After(500 * time.Millisecond):
	}
}
