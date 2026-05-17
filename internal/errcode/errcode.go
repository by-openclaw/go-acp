// Package errcode is the layered error-code taxonomy for dhs.
//
// Every error site in the codebase wraps a typed *Code so callers (CLI,
// scripts, Ansible) can dispatch on a stable <layer>:<code> string without
// parsing free-text messages. The contract is uniform across every connector
// (Ember+, ACP1, ACP2, Probel, AMWA, ...) per memory
// feedback_error_contract_cross_os.
//
// Wire shape on stderr:
//
//	<layer>:<code>: <human message>
//
// Examples:
//
//	transport:refused: connect 127.0.0.1:9100: connection refused
//	validation:out-of-range-low: value -200 below minimum -100
//	matrix:target-locked: target 2 is locked (spec p.89 ConnectionDisposition)
//
// CLI exit code: 0 on success, 2 on usage/validation, 1 on every other error.
// Cross-OS uniform — PowerShell $LASTEXITCODE, Bash $?, cmd.exe %ERRORLEVEL%
// all parse identically.
//
// Usage:
//
//	// Define a typed code in your layer package.
//	var ErrRefused = errcode.New(errcode.LayerTransport, "refused", errcode.ClassRuntime)
//
//	// Wrap at the error site.
//	if err != nil {
//	    return fmt.Errorf("%w: connect %s: %v", ErrRefused, addr, err)
//	}
//
//	// Dispatch at the call site.
//	if errors.Is(err, transport.ErrRefused) { ... }
//	if code := errcode.From(err); code != nil { ... }
//	os.Exit(errcode.Exit(err))
package errcode

import (
	"errors"
	"fmt"
)

// Layer is the system layer that owns a code. Layers map to package
// boundaries: transport, codec sublayers (s101/glow/ber/matrix), plugin
// state, session, validation, and per-protocol invocation errors.
type Layer string

const (
	LayerTransport  Layer = "transport"
	LayerS101       Layer = "s101"
	LayerGlow       Layer = "glow"
	LayerBER        Layer = "ber"
	LayerMatrix     Layer = "matrix"
	LayerEmberplus  Layer = "emberplus"
	LayerACP1       Layer = "acp1"
	LayerACP2       Layer = "acp2"
	LayerProbel     Layer = "probel"
	LayerValidation Layer = "validation"
	LayerPlugin     Layer = "plugin"
	LayerSession    Layer = "session"
)

// Class is the exit-code class for the CLI. Per
// feedback_error_contract_cross_os the contract is Unix-standard 0/1/2 only.
// Never 3+.
type Class int

const (
	// ClassSuccess is exit 0 — verb completed.
	ClassSuccess Class = 0
	// ClassRuntime is exit 1 — any runtime / wire / protocol error.
	// Caller reads stderr for the precise <layer>:<code>.
	ClassRuntime Class = 1
	// ClassUsage is exit 2 — Go flag parse failure, validation:* codes,
	// plugin:object-not-found, plugin:not-connected — caller's input fault.
	ClassUsage Class = 2
)

// Code is a typed error code. Used as a sentinel: wrap at the error site
// with fmt.Errorf("%w: %s", code, dynamic) so errors.Is(err, code) walks
// the chain.
//
// The stringified form is "<layer>:<name>" — that's the stable prefix
// that scripts grep on. The dynamic detail (which path, which value)
// follows after the colon.
type Code struct {
	Layer Layer
	Name  string
	Class Class
}

// New returns a typed error code. Caller owns the returned pointer; the
// usual pattern is one package-level var per code.
func New(layer Layer, name string, class Class) *Code {
	if layer == "" {
		panic("errcode.New: layer is empty")
	}
	if name == "" {
		panic("errcode.New: name is empty")
	}
	return &Code{Layer: layer, Name: name, Class: class}
}

// Error makes *Code satisfy the error interface so it composes with
// fmt.Errorf %w and errors.Is.
func (c *Code) Error() string {
	return fmt.Sprintf("%s:%s", c.Layer, c.Name)
}

// String returns the same form as Error — useful for log fields.
func (c *Code) String() string {
	return c.Error()
}

// From walks the err chain and returns the deepest typed *Code, or nil
// if no typed code is in the chain.
func From(err error) *Code {
	for err != nil {
		if c, ok := err.(*Code); ok {
			return c
		}
		err = errors.Unwrap(err)
	}
	return nil
}

// Exit returns the CLI exit class for err.
//
//   - nil err → 0
//   - typed *Code in chain → that code's Class
//   - any other non-nil err → ClassRuntime (safe fallback)
//
// Callers wire this into os.Exit at the CLI boundary:
//
//	os.Exit(errcode.Exit(err))
func Exit(err error) int {
	if err == nil {
		return int(ClassSuccess)
	}
	if c := From(err); c != nil {
		return int(c.Class)
	}
	return int(ClassRuntime)
}
