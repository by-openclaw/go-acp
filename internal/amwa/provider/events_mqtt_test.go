package provider

// IS-07 MQTT transport: a boot-active MQTT event sender publishes its
// retained connection status + retained state at activation, and every
// SetState reaches the broker — the behaviour IS-07-02 test_06 reads.

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
)

// miniBroker accepts CONNECT (CONNACK), answers PINGREQ, records
// PUBLISH packets.
type miniBroker struct {
	ln net.Listener
	mu sync.Mutex
	// topic -> latest payload + retain flag
	latest map[string]miniMsg
}

type miniMsg struct {
	Payload string
	Retain  bool
}

func newMiniBroker(t *testing.T) *miniBroker {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	b := &miniBroker{ln: ln, latest: map[string]miniMsg{}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go b.serve(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return b
}

func (b *miniBroker) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	one := make([]byte, 1)
	for {
		if _, err := io.ReadFull(conn, one); err != nil {
			return
		}
		fixed := one[0]
		n, mult := 0, 1
		for {
			if _, err := io.ReadFull(conn, one); err != nil {
				return
			}
			n += int(one[0]&0x7F) * mult
			if one[0]&0x80 == 0 {
				break
			}
			mult *= 128
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}
		switch fixed >> 4 {
		case 1: // CONNECT
			if _, err := conn.Write([]byte{0x20, 2, 0, 0}); err != nil {
				return
			}
		case 12: // PINGREQ
			if _, err := conn.Write([]byte{0xD0, 0}); err != nil {
				return
			}
		case 3: // PUBLISH
			if len(body) < 2 {
				return
			}
			tl := int(binary.BigEndian.Uint16(body))
			if len(body) < 2+tl {
				return
			}
			b.mu.Lock()
			b.latest[string(body[2:2+tl])] = miniMsg{Payload: string(body[2+tl:]), Retain: fixed&0x01 != 0}
			b.mu.Unlock()
		}
	}
}

func (b *miniBroker) get(topic string) (miniMsg, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.latest[topic]
	return m, ok
}

func (b *miniBroker) hostPort(t *testing.T) (string, int) {
	t.Helper()
	host, portStr, _ := net.SplitHostPort(b.ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return host, port
}

const (
	mqttTallySourceID = "eeeeeeee-5555-4555-8555-555555555555" // tallyBundle's source
	mqttTallySenderID = "abababab-9999-4999-8999-999999999999"
	mqttStateTopic    = "x-nmos/events/v1.0/sources/" + mqttTallySourceID
	mqttStatusTopic   = "x-nmos/events/v1.0/connection_status/" + mqttTallySourceID
)

// mqttTallyBundle: tallyBundle + an MQTT sender on the tally flow,
// boot-active against the given broker.
func mqttTallyBundle(brokerHost string, brokerPort int) *NodeConfig {
	b := tallyBundle()
	dev := b.Devices[0].ID
	flowID := "ffffffff-6666-4666-8666-666666666666"
	snd := is04.Sender{
		ResourceCore: is04.ResourceCore{
			ID: mqttTallySenderID, Version: "0:0",
			Label: "snd-tally-mqtt", Description: "mqtt tally", Tags: map[string][]string{},
		},
		FlowID: &flowID, Transport: is04.TransportMQTT, DeviceID: dev,
		InterfaceBindings: []string{"eth0"},
	}
	b.Senders = append(b.Senders, snd)
	b.Devices[0].Senders = append(b.Devices[0].Senders, snd.ID)
	b.Events = &EventsSeed{MQTTBroker: brokerHost + ":" + strconv.Itoa(brokerPort)}
	if b.Connection == nil {
		b.Connection = &ConnectionSeed{}
	}
	if b.Connection.Senders == nil {
		b.Connection.Senders = map[string]*EndpointSeed{}
	}
	b.Connection.Senders[snd.ID] = &EndpointSeed{
		MasterEnable: true,
		TransportParams: []is05.TransportParams{{
			"destination_host": brokerHost,
			"destination_port": brokerPort,
			// broker_topic left unset: the device must choose the
			// spec's recommended convention at activation.
		}},
	}
	return b
}

func TestMQTTEventSenderPublishesOnBoot(t *testing.T) {
	broker := newMiniBroker(t)
	host, port := broker.hostPort(t)
	addr := serveNCPBundleNode(t, mqttTallyBundle(host, port))

	// Retained connection status: {"message_type":"connection_status","active":true}.
	waitCond(t, func() bool { _, ok := broker.get(mqttStatusTopic); return ok })
	status, _ := broker.get(mqttStatusTopic)
	if !status.Retain || !strings.Contains(status.Payload, `"connection_status"`) || !strings.Contains(status.Payload, `"active":true`) {
		t.Errorf("connection status = %+v", status)
	}

	// Retained state message for the source.
	waitCond(t, func() bool { _, ok := broker.get(mqttStateTopic); return ok })
	state, _ := broker.get(mqttStateTopic)
	if !state.Retain {
		t.Error("state message not retained")
	}
	var msg map[string]any
	if err := json.Unmarshal([]byte(state.Payload), &msg); err != nil {
		t.Fatalf("state payload: %v (%s)", err, state.Payload)
	}
	if msg["message_type"] != "state" {
		t.Errorf("message_type = %v", msg["message_type"])
	}
	identity, _ := msg["identity"].(map[string]any)
	if identity["source_id"] != mqttTallySourceID {
		t.Errorf("identity = %v", identity)
	}

	// The ACTIVE params carry the chosen topics + the ext param.
	st, raw := mxlGet(t, "http://"+addr+"/x-nmos/connection/v1.2/single/senders/"+mqttTallySenderID+"/active")
	if st != 200 {
		t.Fatalf("active GET = %d", st)
	}
	var active struct {
		TransportParams []map[string]any `json:"transport_params"`
	}
	if err := json.Unmarshal(raw, &active); err != nil || len(active.TransportParams) == 0 {
		t.Fatalf("active decode: %v", err)
	}
	p := active.TransportParams[0]
	if p["broker_topic"] != mqttStateTopic || p["connection_status_broker_topic"] != mqttStatusTopic {
		t.Errorf("active topics = %v / %v", p["broker_topic"], p["connection_status_broker_topic"])
	}
	if _, has := p["ext_is_07_rest_api_url"]; !has {
		t.Error("active params miss ext_is_07_rest_api_url")
	}
}

func TestMQTTStateChangeReachesBroker(t *testing.T) {
	broker := newMiniBroker(t)
	host, port := broker.hostPort(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := mqttTallyBundle(host, port)

	ev := NewIS07EventsServer(logger, b, IS07EventsConfig{APIVer: "v1.0"})
	bridge := newMQTTEventBridge(logger, b, ev.StateMessage)
	if bridge == nil {
		t.Fatal("bridge not built for an MQTT event bundle")
	}
	t.Cleanup(bridge.Close)
	ev.onStateChanged = bridge.OnStateChanged

	bridge.OnSenderActivation(mqttTallySenderID, is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		TransportParams: []is05.TransportParams{{
			"destination_host":               host,
			"destination_port":               port,
			"broker_topic":                   mqttStateTopic,
			"connection_status_broker_topic": mqttStatusTopic,
		}},
	})
	waitCond(t, func() bool { _, ok := broker.get(mqttStatusTopic); return ok })

	if _, ok := ev.SetState(mqttTallySourceID, true); !ok {
		t.Fatal("SetState refused")
	}
	waitCond(t, func() bool {
		m, ok := broker.get(mqttStateTopic)
		return ok && strings.Contains(m.Payload, `"value":true`)
	})
}

// TestMQTTSenderConstraintsMatchStaged: IS-05-01 test_09 compares the
// constraint key set to the staged key set per leg — they must be
// identical (the WS-era constraint back-fill once grew
// ext_is_07_source_id onto MQTT constraints that never stage it).
func TestMQTTSenderConstraintsMatchStaged(t *testing.T) {
	b := mqttTallyBundle("127.0.0.1", 1884)
	st := newConnectionStore()
	st.seedFromBundle(b)
	e, err := st.get("senders", mqttTallySenderID)
	if err != nil {
		t.Fatalf("mqtt endpoint: %v", err)
	}
	for i := range e.staged.TransportParams {
		for k := range e.staged.TransportParams[i] {
			if _, ok := e.constraints[i][k]; !ok {
				t.Errorf("leg %d: staged key %q missing from constraints", i, k)
			}
		}
		for k := range e.constraints[i] {
			if _, ok := e.staged.TransportParams[i][k]; !ok {
				t.Errorf("leg %d: constraint key %q not staged", i, k)
			}
		}
	}
}

func waitCond(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition never met")
}
