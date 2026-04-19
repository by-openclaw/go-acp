package export

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"acp/internal/export/canonical"
)

// ErrNilExport is returned by WriteCanonicalJSON when the caller
// passes a nil *canonical.Export.
var ErrNilExport = errors.New("export: nil")

// WriteCanonicalJSON serialises a canonical Export to w as
// pretty-printed JSON (2-space indent). It honours context
// cancellation by checking ctx.Err() before the write begins —
// during long writes, cancellation is propagated by closing the
// underlying w (the caller's responsibility, typically
// http.ResponseWriter / net.Conn cancellation).
//
// Returns a wrapped error in every failure path:
//   - ErrNilExport              — caller passed nil
//   - context.Canceled/DeadlineExceeded — ctx already done
//   - "json encode export: %w"  — marshalling failure
//   - "write canonical: %w"     — io.Writer returned an error
//
// This function exists alongside the legacy WriteJSON so existing
// CLI paths keep emitting the legacy hierarchical shape until they
// are migrated (step 3 / step 4 per scope).
func WriteCanonicalJSON(ctx context.Context, w io.Writer, e *canonical.Export) error {
	if e == nil {
		return ErrNilExport
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write canceled: %w", err)
	}

	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return fmt.Errorf("json encode export: %w", err)
	}
	// MarshalIndent does not append a trailing newline; add one for
	// POSIX friendliness — consistent with json.Encoder.Encode.
	data = append(data, '\n')

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write canonical: %w", err)
	}
	return nil
}

// ReadCanonicalJSON parses a canonical Export from r. The returned
// *Export has Root populated (Node/Parameter/Matrix/Function, chosen
// per the dispatch rules in canonical/common.go) and Templates
// populated when the input contained a top-level "templates" array.
//
// Context cancellation is honoured on entry and after the full read
// completes. The reader itself is consumed synchronously via
// io.ReadAll — for cancellable reads, wrap r in a
// ctxio-style adapter at the caller.
//
// Returns a wrapped error in every failure path:
//   - context.Canceled/DeadlineExceeded — ctx already done
//   - "read canonical: %w"     — io.Reader returned an error
//   - "parse canonical: %w"    — JSON not a canonical Export
func ReadCanonicalJSON(ctx context.Context, r io.Reader) (*canonical.Export, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read canceled: %w", err)
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read canonical: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read canceled: %w", err)
	}

	var e canonical.Export
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("parse canonical: %w", err)
	}
	return &e, nil
}

// UnmarshalCanonicalElement parses a single element (Node /
// Parameter / Matrix / Function) from a JSON byte slice. Exposed so
// importers and tests can round-trip individual fragments without
// wrapping them in an Export envelope first.
func UnmarshalCanonicalElement(data []byte) (canonical.Element, error) {
	el, err := canonical.UnmarshalElement(data)
	if err != nil {
		return nil, fmt.Errorf("parse element: %w", err)
	}
	return el, nil
}
