package transport

// TLS for every dhs client, decided in ONE place.
//
// Before this file each connector rolled its own *tls.Config, and they did
// not agree:
//
//	ccm/consumer          InsecureSkipVerify — no MinVersion
//	cerebrum-nb/consumer  InsecureSkipVerify — no MinVersion
//	transport/ws          ServerName only    — no MinVersion
//	amwa/provider         MinVersion TLS12 + RootCAs
//
// Only the last one is what we would write if asked. Three connectors were
// therefore free to negotiate TLS 1.0 with a peer that offers it, and the
// posture of the whole tool could only be read by grepping four packages.
//
// Hardening a transport is a transport-layer job: it is done once here and
// every protocol that dials over TCP, WebSocket or HTTP inherits it. A
// connector chooses a POSTURE (verify / skip / pin these roots / present
// this client certificate); it never assembles a crypto/tls config itself.

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// MinTLSVersion is the floor handed to crypto/tls on every dhs connection.
// TLS 1.0 and 1.1 are deprecated (RFC 8996) and no device in the fleet needs
// them; a connector that genuinely does must raise it here, in the open,
// rather than quietly omitting MinVersion in its own package.
const MinTLSVersion = tls.VersionTLS12

// TLSOptions is the posture a connector asks for. The zero value means "no
// TLS" — Client returns a nil config, which every dialer already reads as
// plain TCP, so adding this type changes nothing for a plaintext connector.
//
// Named to sit beside ws.DialOptions / ws.AcceptOptions: it is options for a
// transport, not a second crypto/tls type.
type TLSOptions struct {
	// Enable turns TLS on. False ⇒ Client returns (nil, nil).
	Enable bool

	// Insecure skips certificate verification. It exists because parts of
	// the fleet ship self-signed certificates, but it is never the default:
	// a connector has to ask for it, and the operator sees the flag that
	// asked.
	Insecure bool

	// ServerName overrides the name verified against the certificate.
	// Empty ⇒ the dialer fills in the host it connected to.
	ServerName string

	// RootCAs pins the trust anchors. Nil ⇒ the system pool. Takes
	// precedence over CAFile when both are set.
	RootCAs *x509.CertPool

	// CAFile is a PEM bundle read into the trust anchors — the file form of
	// RootCAs, for a connector configured from the CLI rather than in code.
	CAFile string

	// CertFile + KeyFile present a client certificate (mutual TLS). Both
	// must be set together.
	CertFile string
	KeyFile  string

	// Certificates presents an ALREADY-LOADED client certificate — the
	// in-memory form of CertFile + KeyFile.
	//
	// It exists for a caller whose identity is not a file: the BCP-003-03
	// certificate manager enrols over EST and holds the result in memory,
	// renewing it on a timer, so there is no path to point CertFile at.
	// Takes precedence over CertFile + KeyFile when both are given.
	Certificates []tls.Certificate
}

// Client builds the *tls.Config for an outbound connection.
//
// Returns (nil, nil) when Enable is false — "no TLS", not an error — so a
// caller can hand the result straight to a dialer whose nil case is plain
// TCP.
func (o TLSOptions) Client() (*tls.Config, error) {
	if !o.Enable {
		return nil, nil
	}
	cfg := &tls.Config{
		MinVersion: MinTLSVersion,
		ServerName: o.ServerName,
		//nolint:gosec // G402: opt-in only, and never the zero value — see
		// the Insecure field comment.
		InsecureSkipVerify: o.Insecure,
	}

	roots, err := o.roots()
	if err != nil {
		return nil, err
	}
	cfg.RootCAs = roots

	certs, err := o.clientCert()
	if err != nil {
		return nil, err
	}
	cfg.Certificates = certs

	return cfg, nil
}

// roots resolves the trust anchors: the in-memory pool if given, else the
// PEM bundle at CAFile, else nil (the system pool).
func (o TLSOptions) roots() (*x509.CertPool, error) {
	if o.RootCAs != nil {
		return o.RootCAs, nil
	}
	if o.CAFile == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(o.CAFile)
	if err != nil {
		return nil, fmt.Errorf("transport: read CA file %q: %w", o.CAFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		// A CA file that parses to nothing would silently fall back to the
		// system pool and verify against the wrong anchors. Fail instead.
		return nil, fmt.Errorf("transport: CA file %q contains no certificate",
			o.CAFile)
	}
	return pool, nil
}

// clientCert resolves the mutual-TLS identity: the in-memory certificates if
// given, else the keypair at CertFile + KeyFile, else none.
func (o TLSOptions) clientCert() ([]tls.Certificate, error) {
	if len(o.Certificates) > 0 {
		return o.Certificates, nil
	}
	if o.CertFile == "" && o.KeyFile == "" {
		return nil, nil
	}
	if o.CertFile == "" || o.KeyFile == "" {
		return nil, fmt.Errorf(
			"transport: client certificate needs both CertFile and KeyFile")
	}
	cert, err := tls.LoadX509KeyPair(o.CertFile, o.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("transport: load client certificate: %w", err)
	}
	return []tls.Certificate{cert}, nil
}

// Server builds the *tls.Config for an inbound listener.
//
// Returns (nil, nil) when Enable is false, matching Client, so a caller can
// hand the result straight to a listener whose nil case is plain TCP.
//
// CertFile + KeyFile are this side's identity and are REQUIRED when Enable is
// set — a TLS server with no certificate is not a degraded server, it cannot
// complete a handshake at all, so this fails loudly rather than at the first
// connection.
//
// RootCAs / CAFile mean something different on this side than on Client's:
// they become the trust anchors for CLIENT certificates, and setting either
// turns mutual TLS on. That coupling is deliberate. A server that pins client
// CAs but still accepts anonymous clients has pinned nothing, so asking for
// the anchors is taken as asking for them to be enforced.
func (o TLSOptions) Server() (*tls.Config, error) {
	if !o.Enable {
		return nil, nil
	}
	certs, err := o.clientCert()
	if err != nil {
		return nil, err
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf(
			"transport: TLS server needs CertFile and KeyFile")
	}
	cfg := &tls.Config{MinVersion: MinTLSVersion, Certificates: certs}

	clientCAs, err := o.roots()
	if err != nil {
		return nil, err
	}
	if clientCAs != nil {
		cfg.ClientCAs = clientCAs
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg, nil
}
