package kratos

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	dhsc "dhs/internal/consumer"
	"dhs/internal/kratos/codec"
	"dhs/internal/transport"
)

// MQTT (§7): topics are the UIDs, payloads the CVT Value text. The
// client connects to the broker on the same host as the REST API, with
// the REST credentials in the medium and high models (§6.1).

const (
	mqttKeepAlive  = 30 * time.Second
	mqttPacketMax  = 1 << 20
	mqttAckTimeout = 5 * time.Second
)

type subscription struct {
	filter string
	fn     dhsc.EventFunc
}

type mqttSession struct {
	conn    net.Conn
	writeMu sync.Mutex

	mu      sync.Mutex
	subs    map[string]subscription // by filter
	nextID  uint16
	acks    map[uint16]chan []byte // SUBACK / UNSUBACK return codes by packet id
	done    chan struct{}
	onClose func()
	once    sync.Once
	byUID   func(uid string) (dhsc.Object, bool)
	metricsRx, metricsTx func(int)
}

// Subscribe registers fn for the values under req.Path: a value UID, a
// subtree (every topic beneath it), or the whole tree when Path and
// Label are empty. The first call opens the broker session.
func (p *Plugin) Subscribe(req dhsc.ValueRequest, fn dhsc.EventFunc) error {
	filter, err := p.filterFor(req)
	if err != nil {
		return err
	}
	m, err := p.mqttSession()
	if err != nil {
		return err
	}
	return m.subscribe(filter, fn)
}

// Unsubscribe removes the subscription of req.
func (p *Plugin) Unsubscribe(req dhsc.ValueRequest) error {
	filter, err := p.filterFor(req)
	if err != nil {
		return err
	}
	p.mu.Lock()
	m := p.mqtt
	p.mu.Unlock()
	if m == nil {
		return nil
	}
	return m.unsubscribe(filter)
}

// filterFor maps a request to an MQTT filter. A UID that names a value
// is subscribed exactly; anything else (a node, the root, a UID ending
// in "#" as the SVT writes arrays) gets "/#" so every topic beneath
// arrives. A Path ending in "/#" or "/+" is passed as typed.
func (p *Plugin) filterFor(req dhsc.ValueRequest) (string, error) {
	if req.Slot > 0 { // -1 = "every slot" (neutral watch); the IDU has slot 0 only
		return "", fmt.Errorf("kratos: slot %d: an NGC2 IDU has slot 0 only", req.Slot)
	}
	root := p.Root()
	path := strings.TrimSpace(req.Path)
	if path == "" && req.Label != "" {
		uid, err := p.resolve(req)
		if err != nil {
			return "", err
		}
		return uid, nil
	}
	if strings.HasSuffix(path, "/#") || strings.HasSuffix(path, "/+") || path == "#" {
		if root != "" && !strings.HasPrefix(path, root+"/") && path != "#" {
			return root + "/" + strings.TrimLeft(path, "/"), nil
		}
		return path, nil
	}
	uid := p.uid(path)
	if uid == "" {
		return "#", nil
	}
	if strings.HasSuffix(uid, "#") {
		return strings.TrimSuffix(uid, "#") + "#", nil
	}
	p.mu.Lock()
	_, isValue := p.byUID[uid]
	walked := p.byUID != nil
	p.mu.Unlock()
	if isValue {
		return uid, nil
	}
	if walked || uid == root || path == "" {
		return uid + "/#", nil
	}
	// Not walked: a UID with no children is subscribed as itself AND as
	// a subtree — MQTT "a/#" also matches "a" (MQTT 3.1.1 §4.7.1.2).
	return uid + "/#", nil
}

func (p *Plugin) mqttSession() (*mqttSession, error) {
	p.mu.Lock()
	if p.mqtt != nil {
		m := p.mqtt
		p.mu.Unlock()
		return m, nil
	}
	host, port := p.host, p.port
	p.mu.Unlock()
	if host == "" {
		return nil, fmt.Errorf("kratos: not connected")
	}
	o := p.options(port)
	mport := o.MQTTPort
	if mport == 0 {
		mport = DefaultMQTTPort
		if o.TLS {
			mport = SecureMQTTPort
		}
	}
	addr := net.JoinHostPort(host, strconv.Itoa(mport))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := p.deps.Net.Dial(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("kratos: MQTT broker %s: %w (set %s if the broker listens elsewhere; §7 names 1822, §6.1 1883)", addr, err, EnvMQTTPort)
	}
	if o.TLS {
		cfg, err := transport.TLSOptions{Enable: true, Insecure: o.Insecure}.Client()
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		cfg = cfg.Clone()
		cfg.ServerName = host
		tc := tls.Client(conn, cfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("kratos: MQTT TLS %s: %w", addr, err)
		}
		conn = tc
	}
	m := &mqttSession{
		conn: conn, subs: map[string]subscription{}, acks: map[uint16]chan []byte{},
		done: make(chan struct{}),
		byUID: func(uid string) (dhsc.Object, bool) {
			p.mu.Lock()
			defer p.mu.Unlock()
			o, ok := p.byUID[uid]
			return o, ok
		},
	}
	if mc := p.Metrics(); mc != nil {
		m.metricsRx = mc.ObserveRx
		m.metricsTx = func(n int) { mc.ObserveTx(n, 0) }
	}
	clientID := fmt.Sprintf("dhs-%d", time.Now().UnixNano()%1_000_000_000)
	if err := m.send(codec.Connect(clientID, o.User, o.Pass, uint16(mqttKeepAlive/time.Second))); err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(mqttAckTimeout))
	pkt, err := codec.ReadPacket(conn, mqttPacketMax)
	if err == nil {
		err = codec.ParseConnack(pkt)
	}
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("kratos: MQTT %s: %w", addr, err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	m.onClose = func() {
		p.mu.Lock()
		if p.mqtt == m {
			p.mqtt = nil
		}
		p.mu.Unlock()
	}
	go m.readLoop()
	go m.keepAlive()

	p.mu.Lock()
	if p.mqtt != nil { // lost a race with another Subscribe
		other := p.mqtt
		p.mu.Unlock()
		m.close()
		return other, nil
	}
	p.mqtt = m
	p.mu.Unlock()
	return m, nil
}

func (m *mqttSession) send(b []byte) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	_ = m.conn.SetWriteDeadline(time.Now().Add(mqttAckTimeout))
	_, err := m.conn.Write(b)
	if err == nil && m.metricsTx != nil {
		m.metricsTx(len(b))
	}
	return err
}

func (m *mqttSession) request(build func(id uint16) []byte) ([]byte, error) {
	m.mu.Lock()
	m.nextID++
	if m.nextID == 0 {
		m.nextID = 1
	}
	id := m.nextID
	ch := make(chan []byte, 1)
	m.acks[id] = ch
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.acks, id)
		m.mu.Unlock()
	}()
	if err := m.send(build(id)); err != nil {
		return nil, err
	}
	select {
	case codes := <-ch:
		return codes, nil
	case <-m.done:
		return nil, fmt.Errorf("kratos: MQTT session closed")
	case <-time.After(mqttAckTimeout):
		return nil, fmt.Errorf("kratos: MQTT: no acknowledge within %s", mqttAckTimeout)
	}
}

func (m *mqttSession) subscribe(filter string, fn dhsc.EventFunc) error {
	codes, err := m.request(func(id uint16) []byte { return codec.Subscribe(id, filter) })
	if err != nil {
		return err
	}
	if len(codes) == 0 || codes[0] == 0x80 {
		return fmt.Errorf("kratos: MQTT broker refused the subscription %q", filter)
	}
	m.mu.Lock()
	m.subs[filter] = subscription{filter: filter, fn: fn}
	m.mu.Unlock()
	return nil
}

func (m *mqttSession) unsubscribe(filter string) error {
	m.mu.Lock()
	_, ok := m.subs[filter]
	delete(m.subs, filter)
	m.mu.Unlock()
	if !ok {
		return nil
	}
	_, err := m.request(func(id uint16) []byte { return codec.Unsubscribe(id, filter) })
	return err
}

func (m *mqttSession) readLoop() {
	defer m.close()
	for {
		pkt, err := codec.ReadPacket(m.conn, mqttPacketMax)
		if err != nil {
			return
		}
		if m.metricsRx != nil {
			m.metricsRx(len(pkt.Body) + 2)
		}
		switch pkt.Type {
		case codec.PacketSUBACK:
			if id, codes, err := codec.ParseSuback(pkt); err == nil {
				m.ack(id, codes)
			}
		case codec.PacketUNSUBACK:
			if len(pkt.Body) >= 2 {
				m.ack(uint16(pkt.Body[0])<<8|uint16(pkt.Body[1]), []byte{0})
			}
		case codec.PacketPUBLISH:
			pub, err := codec.ParsePublish(pkt)
			if err != nil {
				continue
			}
			if pub.QoS == 1 {
				_ = m.send(codec.Puback(pub.PacketID))
			}
			m.dispatch(pub)
		}
	}
}

func (m *mqttSession) ack(id uint16, codes []byte) {
	m.mu.Lock()
	ch := m.acks[id]
	m.mu.Unlock()
	if ch != nil {
		select {
		case ch <- codes:
		default:
		}
	}
}

func (m *mqttSession) dispatch(pub codec.Publish) {
	m.mu.Lock()
	var fns []dhsc.EventFunc
	for f, s := range m.subs {
		if codec.TopicMatches(f, pub.Topic) {
			fns = append(fns, s.fn)
		}
	}
	m.mu.Unlock()
	if len(fns) == 0 {
		return
	}
	path := strings.Split(pub.Topic, "/")
	ev := dhsc.Event{
		Path:      pub.Topic,
		Label:     path[len(path)-1],
		Group:     strings.Join(path[:len(path)-1], "/"),
		Value:     dhsc.Value{Kind: dhsc.KindString, Str: string(pub.Payload), Raw: pub.Payload},
		Timestamp: time.Now(),
		Access:    0x03,
	}
	if o, ok := m.byUID(pub.Topic); ok {
		ev.Unit = o.Unit
		ev.ID = o.ID
		if d, ok := o.Meta["name"].(string); ok {
			ev.Description = d
		}
	}
	for _, fn := range fns {
		fn(ev)
	}
}

func (m *mqttSession) keepAlive() {
	t := time.NewTicker(mqttKeepAlive / 2)
	defer t.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-t.C:
			if err := m.send(codec.Pingreq()); err != nil {
				m.close()
				return
			}
		}
	}
}

func (m *mqttSession) close() {
	m.once.Do(func() {
		_ = m.send(codec.Disconnect())
		close(m.done)
		_ = m.conn.Close()
		if m.onClose != nil {
			m.onClose()
		}
	})
}
