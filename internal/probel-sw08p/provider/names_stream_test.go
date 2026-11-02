package probelsw08p

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/plugin"
	"dhs/internal/probel-sw08p/codec"
)

func serverWithMatrix(t *testing.T, targets, sources int) *server {
	t.Helper()
	exp := &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{
				Number: 1, Identifier: "router", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header: canonical.Header{Number: 1, Identifier: "matrix-0", OID: "1.1"},
						Type:   canonical.MatrixOneToN, Mode: canonical.ModeLinear,
						TargetCount: int64(targets), SourceCount: int64(sources),
						Labels: []canonical.MatrixLabel{{BasePath: "router.matrix-0.video"}},
					},
				},
			},
		},
	}
	return newServer(plugin.Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, exp)
}

// An "all names" request is answered with every name of the table, over as
// many response messages as it takes (SW-P-08 §3.1.18 / §3.1.20 / §3.1.24:
// "one or more … RESPONSE messages"). A 70-entry table at each name width a
// request can ask for:
// 32, 16 and 10 names per message, ascending first ids, 70 names in all.
func TestAllNamesAreSentOverAsManyMessagesAsItTakes(t *testing.T) {
	const entries = 70
	srv := serverWithMatrix(t, entries, entries)

	type page struct {
		first int
		names []string
	}
	kinds := []struct {
		name    string
		request func(codec.NameLength) codec.Frame
		decode  func(codec.Frame) (page, error)
		prefix  string
	}{
		{"sources (rx 100 -> tx 106)",
			func(n codec.NameLength) codec.Frame {
				return codec.EncodeAllSourceNamesRequest(codec.AllSourceNamesRequestParams{NameLength: n})
			},
			func(f codec.Frame) (page, error) {
				d, err := codec.DecodeSourceNamesResponse(f)
				return page{int(d.FirstSourceID), d.Names}, err
			}, "SRC"},
		{"destination associations (rx 102 -> tx 107)",
			func(n codec.NameLength) codec.Frame {
				return codec.EncodeAllDestAssocNamesRequest(codec.AllDestAssocNamesRequestParams{NameLength: n})
			},
			func(f codec.Frame) (page, error) {
				d, err := codec.DecodeDestAssocNamesResponse(f)
				return page{int(d.FirstDestAssociationID), d.Names}, err
			}, "DST"},
		{"source associations (rx 114 -> tx 116)",
			func(n codec.NameLength) codec.Frame {
				return codec.EncodeAllSourceAssocNamesRequest(codec.AllSourceAssocNamesRequestParams{NameLength: n})
			},
			func(f codec.Frame) (page, error) {
				d, err := codec.DecodeSourceAssocNamesResponse(f)
				return page{int(d.FirstSourceAssociationID), d.Names}, err
			}, "SRC"},
	}
	for _, k := range kinds {
		for _, width := range []codec.NameLength{codec.NameLen4, codec.NameLen8, codec.NameLen12} {
			res, err := srv.handle(k.request(width))
			if err != nil {
				t.Fatalf("%s, width %d: %v", k.name, width.Bytes(), err)
			}
			if res.reply != nil || res.streamToSender == nil {
				t.Fatalf("%s, width %d: %d names in one message", k.name, width.Bytes(), entries)
			}
			var pages []page
			if err := res.streamToSender(func(f codec.Frame) error {
				p, derr := k.decode(f)
				if derr != nil {
					return derr
				}
				pages = append(pages, p)
				return nil
			}); err != nil {
				t.Fatalf("%s, width %d: %v", k.name, width.Bytes(), err)
			}

			perMsg := width.MaxNamesPerMessage()
			if want := (entries + perMsg - 1) / perMsg; len(pages) != want {
				t.Errorf("%s, width %d: %d message(s), want %d", k.name, width.Bytes(), len(pages), want)
			}
			next := 0
			for _, p := range pages {
				if p.first != next {
					t.Errorf("%s, width %d: a message starts at %d, want %d", k.name, width.Bytes(), p.first, next)
				}
				if len(p.names) > perMsg {
					t.Errorf("%s, width %d: %d names in one message, the width allows %d", k.name, width.Bytes(), len(p.names), perMsg)
				}
				next += len(p.names)
			}
			if next != entries {
				t.Errorf("%s, width %d: %d names sent, want %d", k.name, width.Bytes(), next, entries)
			}
			if width == codec.NameLen8 {
				last := pages[len(pages)-1]
				if got, want := last.names[len(last.names)-1], k.prefix+" 0070"; got != want {
					t.Errorf("%s: the last name is %q, want %q", k.name, got, want)
				}
			}
		}
	}

	// A session that goes away in the middle ends the stream there.
	res, err := srv.handle(codec.EncodeAllSourceNamesRequest(codec.AllSourceNamesRequestParams{NameLength: codec.NameLen8}))
	if err != nil {
		t.Fatal(err)
	}
	gone := errors.New("session closed")
	sent := 0
	if err := res.streamToSender(func(codec.Frame) error {
		sent++
		if sent == 2 {
			return gone
		}
		return nil
	}); !errors.Is(err, gone) || sent != 2 {
		t.Errorf("stream on a closed session: err %v after %d message(s), want it to stop at the second", err, sent)
	}
}

// firstMessage is the first response message of an "all names" answer,
// whether the table fitted in one message or was sent over several.
func firstMessage(t *testing.T, res handlerResult) codec.Frame {
	t.Helper()
	if res.reply != nil {
		return *res.reply
	}
	if res.streamToSender == nil {
		t.Fatal("no response at all")
	}
	var first *codec.Frame
	_ = res.streamToSender(func(f codec.Frame) error {
		if first == nil {
			first = &f
		}
		return nil
	})
	if first == nil {
		t.Fatal("an empty stream")
	}
	return *first
}
