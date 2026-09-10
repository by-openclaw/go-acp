package main

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"dhs/internal/manifest"
)

// fakeCards stands in for a provider that answers each card on a port of its
// own, recording where it was told to put them.
type fakeCards struct {
	ports []uint8
	dms   []string
	err   error
}

func (f *fakeCards) SetCards(ports []uint8, dms []string) error {
	f.ports, f.dms = ports, dms
	return f.err
}

func TestManifestCardsArePlacedWhereTheManifestSays(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mf := &manifest.Manifest{Frames: []manifest.Frame{{Slots: []manifest.Slot{
		{Addr: map[string]any{"slot": 3}, DM: "IQDBE00@5.0.cs5"},
		{Addr: map[string]any{"slot": 11}, DM: "IQMUX42@8.5.cs17"},
	}}}}

	f := &fakeCards{}
	if err := placeManifestCards(f, mf, logger); err != nil {
		t.Fatalf("placeManifestCards: %v", err)
	}
	if len(f.ports) != 2 || f.ports[0] != 3 || f.ports[1] != 11 ||
		strings.Join(f.dms, ",") != "IQDBE00@5.0.cs5,IQMUX42@8.5.cs17" {
		t.Errorf("placed %v %v", f.ports, f.dms)
	}
}

func TestManifestCardsAreLeftAloneWhereThereIsNothingToPlace(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mf := &manifest.Manifest{Frames: []manifest.Frame{{Slots: []manifest.Slot{
		{Addr: map[string]any{"slot": 1}, DM: "IQDBE00@5.0.cs5"},
	}}}}

	// A provider that numbers its own cards is not told anything.
	if err := placeManifestCards(struct{}{}, mf, logger); err != nil {
		t.Errorf("a provider without cards to place: %v", err)
	}
	// And a frame served from a tree has no manifest to take slots from.
	if err := placeManifestCards(&fakeCards{}, nil, logger); err != nil {
		t.Errorf("no manifest: %v", err)
	}
}

func TestManifestCardsAreRefusedRatherThanGuessedAt(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// A slot that is not a number cannot be a port.
	notASlot := &manifest.Manifest{Frames: []manifest.Frame{{Slots: []manifest.Slot{
		{Addr: map[string]any{"oid": "1.4"}, DM: "IQDBE00@5.0.cs5"},
	}}}}
	if err := placeManifestCards(&fakeCards{}, notASlot, logger); err == nil {
		t.Error("a slot with no number was placed somewhere")
	}

	// And a provider that refuses the placement stops the producer, rather
	// than serving a frame that is not the one the manifest describes.
	mf := &manifest.Manifest{Frames: []manifest.Frame{{Slots: []manifest.Slot{
		{Addr: map[string]any{"slot": 1}, DM: "IQDBE00@5.0.cs5"},
	}}}}
	if err := placeManifestCards(&fakeCards{err: errors.New("refused")}, mf, logger); err == nil {
		t.Error("a refused placement was not reported")
	}
}
