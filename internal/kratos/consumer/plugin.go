// Package kratos is the consumer of the Kratos NGC2 IDU CORE API — the
// controller of a satellite dish antenna (manual 7592827 Rev. E).
//
// State comes from the REST API (§4–§5): the Scalable Value Table (SVT)
// describes every value, the Current Value Table (CVT) holds the values.
// Changes come from the MQTT broker (§7), whose topics are the UIDs.
// Writes are postMessage SET messages; actions ("stow", "peak now") are
// postMessage command verbs (§9). The document defines no acknowledge
// for either: the result is read back.
package kratos

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	dhsc "dhs/internal/consumer"
	"dhs/internal/kratos/codec"
	"dhs/internal/plugin"
	"dhs/internal/transport"
	transporthttp "dhs/internal/transport/http"
)

// Registry name and ports (§2.4, §4.1, §6.1).
const (
	Name             = "kratos"
	DefaultPort      = 3000 // HTTP, low and medium security
	SecurePort       = 4000 // HTTPS, high security
	DefaultMQTTPort  = 1883 // §6.1 scenarios 1–2 (§7 says 1822 — see docs, open question)
	SecureMQTTPort   = 8883 // §6.1 scenario 3, MQTT over TLS
	staleAfter       = 90 * time.Second
	defaultSetSettle = 500 * time.Millisecond
)

// Environment the plugin reads at Connect. Secrets never travel as
// flags (they would land in shell history and ps).
const (
	EnvUser     = "DHS_KRATOS_USER"      // login operator; empty = low-security model, no login
	EnvPass     = "DHS_KRATOS_PASS"      // its password
	EnvTLS      = "DHS_KRATOS_TLS"       // "1": HTTPS + MQTT over TLS (implied by port 4000)
	EnvInsecure = "DHS_KRATOS_INSECURE"  // "1": accept a self-signed certificate
	EnvMQTTPort = "DHS_KRATOS_MQTT_PORT" // broker port override
)

func init() {
	dhsc.Register(&Factory{})
}

// Factory builds Plugin instances.
type Factory struct{}

// Meta describes the plugin to the registry.
func (f *Factory) Meta() dhsc.ProtocolMeta {
	return dhsc.ProtocolMeta{
		Name:        Name,
		DefaultPort: DefaultPort,
		Description: "Kratos NGC2 IDU antenna controller (REST/JSON + MQTT, API 7592827 Rev. E)",
	}
}

// New builds a plugin from the injected dependency set.
func (f *Factory) New(deps plugin.Deps) dhsc.Protocol {
	deps = deps.WithDefaults()
	p := &Plugin{deps: deps, settle: defaultSetSettle}
	p.Init(deps, staleAfter)
	return p
}

// Options is the session configuration; Connect fills it from the
// environment when the caller did not set it (tests set it directly).
type Options struct {
	User, Pass string
	TLS        bool
	Insecure   bool
	MQTTPort   int
}

// Plugin is one NGC2 IDU. A controller is one box: slot 0.
type Plugin struct {
	dhsc.Base

	deps   plugin.Deps
	opts   *Options
	settle time.Duration

	mu     sync.Mutex
	rest   *rest
	host   string
	port   int
	system string // the envelope "system" id of this IDU
	cust   string // the envelope customerId
	user   string // logged-in operator, "" anonymous
	root   string // the UID root, learned from the SVT ("cpiasd")
	tree   []dhsc.Object
	byUID  map[string]dhsc.Object
	mqtt   *mqttSession
}

// SetOptions fixes the session configuration instead of the environment.
func (p *Plugin) SetOptions(o Options) { p.opts = &o }

// PathNative: a --path is a UID, answered by cvtItem without a walk.
func (p *Plugin) PathNative() bool { return true }

func (p *Plugin) options(port int) Options {
	if p.opts != nil {
		return *p.opts
	}
	o := Options{
		User:     os.Getenv(EnvUser),
		Pass:     os.Getenv(EnvPass),
		TLS:      os.Getenv(EnvTLS) == "1" || port == SecurePort,
		Insecure: os.Getenv(EnvInsecure) == "1",
	}
	if n, err := strconv.Atoi(os.Getenv(EnvMQTTPort)); err == nil && n > 0 {
		o.MQTTPort = n
	}
	return o
}

// Connect follows the client sequence of §2.5: login when credentials
// are set, then one small read (boundaries) proves the session and
// gives the system id and the UID root.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	if port <= 0 {
		port = DefaultPort
	}
	o := p.options(port)
	scheme := "http"
	client := transporthttp.NewClient()
	if o.TLS {
		scheme = "https"
		c, err := transporthttp.NewTLSClient(transport.TLSOptions{Enable: true, Insecure: o.Insecure}, transporthttp.DefaultTimeout)
		if err != nil {
			return fmt.Errorf("kratos: tls: %w", err)
		}
		client = c
	}
	client.Metrics = p.Metrics()
	client.Recorder = p.Recorder()
	r := newREST(fmt.Sprintf("%s://%s:%d", scheme, ip, port), client)

	if o.User != "" {
		if _, err := r.login(ctx, o.User, o.Pass); err != nil {
			return err
		}
	}
	env, err := r.get(ctx, "consumer/boundaries")
	if err != nil {
		return fmt.Errorf("kratos: %s:%d: %w", ip, port, err)
	}
	var bounds []codec.Boundary
	_, _ = env.Rows(&bounds)
	root := ""
	for _, b := range bounds {
		if b.UID != "" && !strings.Contains(b.UID, "/") {
			root = b.UID
			break
		}
	}

	p.mu.Lock()
	p.rest, p.host, p.port = r, ip, port
	p.system, p.cust, p.user, p.root = env.System, env.CustomerID, o.User, root
	p.tree, p.byUID = nil, nil
	old := p.mqtt
	p.mqtt = nil
	p.mu.Unlock()
	if old != nil {
		old.close()
	}
	p.Opened("tcp", ip, port, dhsc.MetricsTimes{C: p.Metrics()})
	return nil
}

// Disconnect logs out (when logged in) and closes MQTT.
func (p *Plugin) Disconnect() error {
	p.mu.Lock()
	r, user, m := p.rest, p.user, p.mqtt
	p.rest, p.mqtt, p.tree, p.byUID = nil, nil, nil, nil
	p.mu.Unlock()
	if m != nil {
		m.close()
	}
	if r != nil && user != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = r.logout(ctx)
		cancel()
	}
	p.Closed()
	return nil
}

func (p *Plugin) session() (*rest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rest == nil {
		return nil, fmt.Errorf("kratos: not connected")
	}
	return p.rest, nil
}

// GetDeviceInfo: one controller, one slot. The API publishes no model or
// version object in the document; the envelope's system id identifies
// the IDU (§4.3).
func (p *Plugin) GetDeviceInfo(ctx context.Context) (dhsc.DeviceInfo, error) {
	if _, err := p.session(); err != nil {
		return dhsc.DeviceInfo{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return dhsc.DeviceInfo{IP: p.host, Port: p.port, NumSlots: 1}, nil
}

// GetSlotInfo reports slot 0 present with the envelope identity.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (dhsc.SlotInfo, error) {
	if slot != 0 {
		return dhsc.SlotInfo{}, fmt.Errorf("kratos: slot %d: an NGC2 IDU has slot 0 only", slot)
	}
	if _, err := p.session(); err != nil {
		return dhsc.SlotInfo{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	id := map[string]string{"system": p.system, "customerId": p.cust, "root": p.root}
	if p.user != "" {
		id["operator"] = p.user
	}
	return dhsc.SlotInfo{Slot: 0, Status: dhsc.SlotPresent, State: dhsc.SlotStatePresent, IsOnline: true, Identity: id}, nil
}

// Root returns the UID root learned at Connect ("" before).
func (p *Plugin) Root() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.root
}

func (p *Plugin) uid(path string) string {
	p.mu.Lock()
	root := p.root
	p.mu.Unlock()
	return codec.UIDFor(root, path)
}
