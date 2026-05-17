// Code sentinels for the Glow decode layer.
//
// Glow sits on top of BER (every element is a BER APPLICATION-tagged
// SEQUENCE / SEQUENCE OF). Most decode failures bubble up from the BER
// layer (ber:truncated, ber:tag-too-long, etc.) and the Glow layer adds
// the schema context.
//
// Wire form: "glow:<code>: <human message>".
//
// R1d migration — adds Glow-level wrap sentinels so callers can distinguish
// "the byte-level BER decode failed" (ber:*) from "the Ember+ schema layer
// rejected the message" (glow:*).
package glow

import "dhs/internal/errcode"

// All glow:* codes are runtime decode failures (Class 1, exit 1).
var (
	// ErrDecodeFailed wraps a top-level DecodeRoot failure. The
	// underlying ber:* error is preserved in the chain so callers can
	// errors.Is against the specific BER cause.
	ErrDecodeFailed = errcode.New(errcode.LayerGlow, "decode-failed", errcode.ClassRuntime)
)
