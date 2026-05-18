// Package dnssd is the DNS-SD / mDNS surface dhs uses for service
// discovery (#477 / R18). The package presents one interface — Browser
// — and ships an exec-based "tooled" backend that wraps the operating
// system's native mDNS browse utility (dns-sd on macOS / Windows,
// avahi-browse on Linux).
//
// The stdlib-floor pure-Go mDNS implementation is staged for v2;
// today the tooled backend is the supported path and is documented
// in the help text of the consuming verbs.
//
// Future backends per memory project_dnssd_multi_os_backend:
//
//	Linux   - Avahi via DBus (avahi-browse for v1, native DBus for v2)
//	macOS   - Bonjour CGo (dns-sd for v1, CGo for v2)
//	Windows - Bonjour Service CGo (dns-sd.exe via Bonjour SDK for v1, CGo v2)
//	Floor   - pure-Go mDNS, no CGo, no external tools
package dnssd

import (
	"context"
	"time"
)

// Service is one discovered mDNS responder.
type Service struct {
	Name     string            // service-instance name as advertised
	Host     string            // resolved A/AAAA address (IPv4 preferred)
	Port     int               // SRV target port
	Hostname string            // mDNS hostname (e.g. host.local.)
	TXT      map[string]string // TXT key/value pairs
}

// BrowseOptions configures one Browse call.
type BrowseOptions struct {
	// ServiceType is the DNS-SD service identifier without the trailing
	// `.local.`, e.g. `_ember._tcp`. Required.
	ServiceType string

	// Duration is how long the browser listens before returning. Zero
	// means "use backend default" (typically 5s).
	Duration time.Duration
}

// Browser is the DNS-SD browse capability every backend implements.
// Implementations return ErrUnsupported when the host's tooling is
// missing; callers translate to validation:mdns-tool-not-found per
// R1 #468.
type Browser interface {
	Browse(ctx context.Context, opts BrowseOptions) ([]Service, error)
}

// Announcer is the DNS-SD announce capability the provider side uses
// (`dhs producer <proto> serve --mdns`). v1 implementations may
// return ErrUnsupported when the host tooling cannot announce
// (e.g. Windows without Bonjour SDK); callers should clamp to a
// startup warning rather than fail Serve.
type Announcer interface {
	Announce(ctx context.Context, svc Service) (stop func(), err error)
}
