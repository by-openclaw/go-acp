package probelsw08p

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dhs/internal/probel-sw08p/codec"
)

// A matrix sized above the general form by the caller (--srcs) is asked in
// the extended form even for a small destination: the EVS Neuron case,
// 4352 sources, dst 5 routed to src 3077.
func TestCrosspointInterrogateExtendedWhenSourcesConfigured(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeCrosspointTally(codec.CrosspointTallyParams{DestinationID: 5, SourceID: 3077}),
	))
	defer cleanup()
	p.SetMatrixConfig(MatrixConfig{Srcs: 4352})
	ctx, cancel := ctxT(t)
	defer cancel()

	got, err := p.CrosspointInterrogate(ctx, 0, 0, 5)
	if err != nil {
		t.Fatalf("CrosspointInterrogate: %v", err)
	}
	if got.SourceID != 3077 {
		t.Errorf("src = %d; want 3077", got.SourceID)
	}
	if req, _ := peer.lastRequest(); req.ID != codec.RxCrosspointInterrogateExt {
		t.Errorf("request ID %#x; want the extended interrogate %#x", req.ID, codec.RxCrosspointInterrogateExt)
	}
}

// 1024 sources is ids 0-1023, which the general form names: nothing changes.
func TestCrosspointInterrogateStaysGeneralAtTheLimit(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeCrosspointTally(codec.CrosspointTallyParams{DestinationID: 5, SourceID: 1023}),
	))
	defer cleanup()
	p.SetMatrixConfig(MatrixConfig{Srcs: generalFormSources})
	ctx, cancel := ctxT(t)
	defer cancel()

	if _, err := p.CrosspointInterrogate(ctx, 0, 0, 5); err != nil {
		t.Fatalf("CrosspointInterrogate: %v", err)
	}
	if req, _ := peer.lastRequest(); req.ID != codec.RxCrosspointInterrogate {
		t.Errorf("request ID %#x; want the general interrogate", req.ID)
	}
}

// The size the matrix reports in its own source-name table is learned, so an
// interrogate later in the same session goes extended without --srcs.
func TestCrosspointInterrogateLearnsSizeFromSourceNames(t *testing.T) {
	names := make([]string, 16) // §3.3.19: at most 16 eight-char names per frame
	for i := range names {
		names[i] = "SRC"
	}
	p, peer, cleanup := newPeerHarness(t, func(req codec.Frame) []codec.Frame {
		switch req.ID {
		case codec.RxAllSourceNamesRequest:
			// Sources 1020-1035: the table ends past the general form.
			return []codec.Frame{codec.EncodeSourceNamesResponse(codec.SourceNamesResponseParams{
				NameLength: codec.NameLen8, FirstSourceID: 1020, Names: names,
			})}
		default:
			return []codec.Frame{codec.EncodeCrosspointTally(codec.CrosspointTallyParams{DestinationID: 5, SourceID: 1035})}
		}
	})
	defer cleanup()
		ctx, cancel := ctxT(t)
	defer cancel()

	got, err := p.AllSourceNames(ctx, 0, 0, codec.NameLen8)
	if err != nil || got.FirstSourceID != 1020 || len(got.Names) != 16 {
		t.Fatalf("AllSourceNames: first=%d names=%d err=%v", got.FirstSourceID, len(got.Names), err)
	}
	if _, err := p.CrosspointInterrogate(ctx, 0, 0, 5); err != nil {
		t.Fatalf("CrosspointInterrogate: %v", err)
	}
	if req, _ := peer.lastRequest(); req.ID != codec.RxCrosspointInterrogateExt {
		t.Errorf("request ID %#x; want extended after learning 1036 sources", req.ID)
	}
}

// A general interrogate that times out says what to do about it; an
// extended one does not, since the size was already accounted for.
func TestCrosspointInterrogateTimeoutExplainsTheGeneralFormLimit(t *testing.T) {
	for _, tc := range []struct {
		name string
		srcs uint16
		hint bool
	}{
		{"general", 0, true},
		{"extended", 4352, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, cleanup := newPeerHarness(t, ackOnly)
			defer cleanup()
			p.SetMatrixConfig(MatrixConfig{Srcs: tc.srcs})
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			_, err := p.CrosspointInterrogate(ctx, 0, 0, 0)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("err = %v; want deadline exceeded", err)
			}
			if got := strings.Contains(err.Error(), "--srcs"); got != tc.hint {
				t.Errorf("hint present = %v; want %v (err: %v)", got, tc.hint, err)
			}
		})
	}
}
