package probelsw08p

import (
	"context"
	"errors"
	"testing"

	"dhs/internal/probel-sw08p/codec"
	"dhs/internal/consumer"
)

// ---- rx 100 All Source Names ----------------------------------------------

func TestAllSourceNames(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeSourceNamesResponse(codec.SourceNamesResponseParams{
			MatrixID: 0, LevelID: 0, NameLength: codec.NameLen8, FirstSourceID: 0,
			Names: []string{"SRC1", "SRC2"},
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.AllSourceNames(ctx, 0, 0, codec.NameLen8)
	if err != nil {
		t.Fatalf("AllSourceNames: %v", err)
	}
	if len(got.Names) != 2 || got.Names[0] != "SRC1" {
		t.Errorf("names = %v; want [SRC1 SRC2]", got.Names)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxAllSourceNamesRequest {
		t.Errorf("req ID %#x; want RxAllSourceNamesRequest", req.ID)
	}
}

func TestAllSourceNamesNotConnected(t *testing.T) {
	_, err := freshPlugin().AllSourceNames(context.Background(), 0, 0, codec.NameLen8)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestAllSourceNamesDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxSourceNamesResponse, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.AllSourceNames(ctx, 0, 0, codec.NameLen8)
	assertTransportDecodeErr(t, err)
}

// ---- rx 101 Single Source Name --------------------------------------------

func TestSingleSourceName(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeSourceNamesResponse(codec.SourceNamesResponseParams{
			NameLength: codec.NameLen8, FirstSourceID: 5, Names: []string{"ONE"},
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	name, err := p.SingleSourceName(ctx, 0, 0, codec.NameLen8, 5)
	if err != nil {
		t.Fatalf("SingleSourceName: %v", err)
	}
	if name != "ONE" {
		t.Errorf("name = %q; want ONE", name)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxSingleSourceNameRequest {
		t.Errorf("req ID %#x; want RxSingleSourceNameRequest", req.ID)
	}
}

func TestSingleSourceNameEmpty(t *testing.T) {
	// A response with zero names → method returns "".
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeSourceNamesResponse(codec.SourceNamesResponseParams{
			NameLength: codec.NameLen8, Names: nil,
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	name, err := p.SingleSourceName(ctx, 0, 0, codec.NameLen8, 0)
	if err != nil {
		t.Fatalf("SingleSourceName: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q; want empty", name)
	}
}

func TestSingleSourceNameNotConnected(t *testing.T) {
	_, err := freshPlugin().SingleSourceName(context.Background(), 0, 0, codec.NameLen8, 0)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestSingleSourceNameDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxSourceNamesResponse, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.SingleSourceName(ctx, 0, 0, codec.NameLen8, 0)
	assertTransportDecodeErr(t, err)
}

// ---- rx 102 All Dest Assoc Names ------------------------------------------

func TestAllDestAssocNames(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeDestAssocNamesResponse(codec.DestAssocNamesResponseParams{
			NameLength: codec.NameLen8, Names: []string{"DST1"},
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.AllDestAssocNames(ctx, 0, codec.NameLen8)
	if err != nil {
		t.Fatalf("AllDestAssocNames: %v", err)
	}
	if len(got.Names) != 1 || got.Names[0] != "DST1" {
		t.Errorf("names = %v; want [DST1]", got.Names)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxAllDestNamesRequest {
		t.Errorf("req ID %#x; want RxAllDestNamesRequest", req.ID)
	}
}

func TestAllDestAssocNamesNotConnected(t *testing.T) {
	_, err := freshPlugin().AllDestAssocNames(context.Background(), 0, codec.NameLen8)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestAllDestAssocNamesDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxDestAssocNamesResponse, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.AllDestAssocNames(ctx, 0, codec.NameLen8)
	assertTransportDecodeErr(t, err)
}

// ---- rx 103 Single Dest Assoc Name ----------------------------------------

func TestSingleDestAssocName(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeDestAssocNamesResponse(codec.DestAssocNamesResponseParams{
			NameLength: codec.NameLen8, FirstDestAssociationID: 2, Names: []string{"OUT"},
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	name, err := p.SingleDestAssocName(ctx, 0, codec.NameLen8, 2)
	if err != nil {
		t.Fatalf("SingleDestAssocName: %v", err)
	}
	if name != "OUT" {
		t.Errorf("name = %q; want OUT", name)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxSingleDestNameRequest {
		t.Errorf("req ID %#x; want RxSingleDestNameRequest", req.ID)
	}
}

func TestSingleDestAssocNameEmpty(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeDestAssocNamesResponse(codec.DestAssocNamesResponseParams{
			NameLength: codec.NameLen8, Names: nil,
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	name, err := p.SingleDestAssocName(ctx, 0, codec.NameLen8, 0)
	if err != nil {
		t.Fatalf("SingleDestAssocName: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q; want empty", name)
	}
}

func TestSingleDestAssocNameNotConnected(t *testing.T) {
	_, err := freshPlugin().SingleDestAssocName(context.Background(), 0, codec.NameLen8, 0)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestSingleDestAssocNameDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxDestAssocNamesResponse, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.SingleDestAssocName(ctx, 0, codec.NameLen8, 0)
	assertTransportDecodeErr(t, err)
}

// ---- rx 112 Tie-Line Interrogate ------------------------------------------

func TestTieLineInterrogate(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeTieLineTally(codec.TieLineTallyParams{
			DestMatrixID: 0, DestAssociationID: 9,
			Sources: []codec.TieLineSource{
				{MatrixID: 0, LevelID: 0, SourceID: 5},
			},
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.TieLineInterrogate(ctx, 0, 9)
	if err != nil {
		t.Fatalf("TieLineInterrogate: %v", err)
	}
	if len(got.Sources) != 1 || got.Sources[0].SourceID != 5 {
		t.Errorf("sources = %+v; want one src=5", got.Sources)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxCrosspointTieLineInterrogate {
		t.Errorf("req ID %#x; want RxCrosspointTieLineInterrogate", req.ID)
	}
}

func TestTieLineInterrogateNotConnected(t *testing.T) {
	_, err := freshPlugin().TieLineInterrogate(context.Background(), 0, 0)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestTieLineInterrogateDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxCrosspointTieLineTally, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.TieLineInterrogate(ctx, 0, 0)
	assertTransportDecodeErr(t, err)
}

// ---- rx 114 All Source Assoc Names ----------------------------------------

func TestAllSourceAssocNames(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeSourceAssocNamesResponse(codec.SourceAssocNamesResponseParams{
			NameLength: codec.NameLen8, Names: []string{"SA1"},
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.AllSourceAssocNames(ctx, 0, codec.NameLen8)
	if err != nil {
		t.Fatalf("AllSourceAssocNames: %v", err)
	}
	if len(got.Names) != 1 || got.Names[0] != "SA1" {
		t.Errorf("names = %v; want [SA1]", got.Names)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxAllSourceAssocNamesRequest {
		t.Errorf("req ID %#x; want RxAllSourceAssocNamesRequest", req.ID)
	}
}

func TestAllSourceAssocNamesNotConnected(t *testing.T) {
	_, err := freshPlugin().AllSourceAssocNames(context.Background(), 0, codec.NameLen8)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestAllSourceAssocNamesDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxSourceAssocNamesResponse, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.AllSourceAssocNames(ctx, 0, codec.NameLen8)
	assertTransportDecodeErr(t, err)
}

// ---- rx 115 Single Source Assoc Name --------------------------------------

func TestSingleSourceAssocName(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeSourceAssocNamesResponse(codec.SourceAssocNamesResponseParams{
			NameLength: codec.NameLen8, FirstSourceAssociationID: 1, Names: []string{"SAX"},
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	name, err := p.SingleSourceAssocName(ctx, 0, codec.NameLen8, 1)
	if err != nil {
		t.Fatalf("SingleSourceAssocName: %v", err)
	}
	if name != "SAX" {
		t.Errorf("name = %q; want SAX", name)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxSingleSourceAssocNameRequest {
		t.Errorf("req ID %#x; want RxSingleSourceAssocNameRequest", req.ID)
	}
}

func TestSingleSourceAssocNameEmpty(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeSourceAssocNamesResponse(codec.SourceAssocNamesResponseParams{
			NameLength: codec.NameLen8, Names: nil,
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	name, err := p.SingleSourceAssocName(ctx, 0, codec.NameLen8, 0)
	if err != nil {
		t.Fatalf("SingleSourceAssocName: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q; want empty", name)
	}
}

func TestSingleSourceAssocNameNotConnected(t *testing.T) {
	_, err := freshPlugin().SingleSourceAssocName(context.Background(), 0, codec.NameLen8, 0)
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestSingleSourceAssocNameDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxSourceAssocNamesResponse, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.SingleSourceAssocName(ctx, 0, codec.NameLen8, 0)
	assertTransportDecodeErr(t, err)
}

// ---- rx 117 Update Name Request (no reply) --------------------------------

func TestUpdateNameRequest(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, ackOnly)
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	err := p.UpdateNameRequest(ctx, codec.UpdateNameRequestParams{
		NameType: codec.UpdateNameSource, NameLength: codec.NameLen8,
		MatrixID: 0, LevelID: 0, FirstID: 1, Names: []string{"NEW"},
	})
	if err != nil {
		t.Fatalf("UpdateNameRequest: %v", err)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxUpdateNameRequest {
		t.Errorf("req ID %#x; want RxUpdateNameRequest", req.ID)
	}
}

func TestUpdateNameRequestNotConnected(t *testing.T) {
	err := freshPlugin().UpdateNameRequest(context.Background(), codec.UpdateNameRequestParams{})
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestUpdateNameRequestSendErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, ackOnly)
	cleanup()
	err := p.UpdateNameRequest(context.Background(), codec.UpdateNameRequestParams{})
	if err == nil {
		t.Fatal("UpdateNameRequest after close returned nil; want send error")
	}
}

// ---- rx 120 Salvo Connect-On-Go -------------------------------------------

func TestSalvoConnectOnGo(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeSalvoConnectOnGoAck(codec.SalvoConnectOnGoAckParams{
			MatrixID: 0, LevelID: 0, DestinationID: 3, SourceID: 4, SalvoID: 1,
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.SalvoConnectOnGo(ctx, codec.SalvoConnectOnGoParams{
		MatrixID: 0, LevelID: 0, DestinationID: 3, SourceID: 4, SalvoID: 1,
	})
	if err != nil {
		t.Fatalf("SalvoConnectOnGo: %v", err)
	}
	if got.DestinationID != 3 || got.SalvoID != 1 {
		t.Errorf("ack = %+v; want dst=3 salvo=1", got)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxCrosspointConnectOnGoSalvo {
		t.Errorf("req ID %#x; want RxCrosspointConnectOnGoSalvo", req.ID)
	}
}

func TestSalvoConnectOnGoNotConnected(t *testing.T) {
	_, err := freshPlugin().SalvoConnectOnGo(context.Background(), codec.SalvoConnectOnGoParams{})
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestSalvoConnectOnGoDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxSalvoConnectOnGoAck, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.SalvoConnectOnGo(ctx, codec.SalvoConnectOnGoParams{})
	assertTransportDecodeErr(t, err)
}

// ---- rx 121 Salvo Go ------------------------------------------------------

func TestSalvoGo(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeSalvoGoDoneAck(codec.SalvoGoDoneAckParams{
			Status: codec.SalvoDoneSet, SalvoID: 1,
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.SalvoGo(ctx, codec.SalvoGoParams{Op: codec.SalvoOpSet, SalvoID: 1})
	if err != nil {
		t.Fatalf("SalvoGo: %v", err)
	}
	if got.Status != codec.SalvoDoneSet || got.SalvoID != 1 {
		t.Errorf("ack = %+v; want set salvo=1", got)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxCrosspointGoSalvo {
		t.Errorf("req ID %#x; want RxCrosspointGoSalvo", req.ID)
	}
}

func TestSalvoGoNotConnected(t *testing.T) {
	_, err := freshPlugin().SalvoGo(context.Background(), codec.SalvoGoParams{})
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestSalvoGoDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxSalvoGoDoneAck, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.SalvoGo(ctx, codec.SalvoGoParams{})
	assertTransportDecodeErr(t, err)
}

// ---- rx 124 Salvo Group Interrogate ---------------------------------------

func TestSalvoGroupInterrogate(t *testing.T) {
	p, peer, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeSalvoGroupTally(codec.SalvoGroupTallyParams{
			MatrixID: 0, LevelID: 0, DestinationID: 2, SourceID: 3,
			SalvoID: 1, ConnectIndex: 0, Validity: codec.SalvoTallyValidLast,
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.SalvoGroupInterrogate(ctx, codec.SalvoGroupInterrogateParams{
		SalvoID: 1, ConnectIndex: 0,
	})
	if err != nil {
		t.Fatalf("SalvoGroupInterrogate: %v", err)
	}
	if got.Validity != codec.SalvoTallyValidLast || got.DestinationID != 2 {
		t.Errorf("tally = %+v; want last, dst=2", got)
	}
	req, _ := peer.lastRequest()
	if req.ID != codec.RxCrosspointSalvoGroupInterrogate {
		t.Errorf("req ID %#x; want RxCrosspointSalvoGroupInterrogate", req.ID)
	}
}

func TestSalvoGroupInterrogateNotConnected(t *testing.T) {
	_, err := freshPlugin().SalvoGroupInterrogate(context.Background(), codec.SalvoGroupInterrogateParams{})
	if !errors.Is(err, consumer.ErrNotConnected) {
		t.Fatalf("err = %v; want ErrNotConnected", err)
	}
}

func TestSalvoGroupInterrogateDecodeErr(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.Frame{ID: codec.TxSalvoGroupTally, Payload: []byte{0x00}},
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	_, err := p.SalvoGroupInterrogate(ctx, codec.SalvoGroupInterrogateParams{})
	assertTransportDecodeErr(t, err)
}
