//go:build !linux

package dnssd

import "log/slog"

// tryDaemonBrowser is the non-Linux stub. macOS / Windows Bonjour is
// staged for a follow-up commit (CGo to dnssd.dll / libSystem). Until
// that lands, every non-Linux platform falls through to stdlibBrowser.
func tryDaemonBrowser(_ *slog.Logger) (Browser, bool) { return nil, false }

// tryDaemonResponder mirrors tryDaemonBrowser for Responder.
func tryDaemonResponder(_ *slog.Logger) (Responder, bool) { return nil, false }
