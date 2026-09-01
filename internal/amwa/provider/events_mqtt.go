// Layer-3 IS-07 Event & Tally — the MQTT transport side.
//
// IS-07 gives a consumer two ways to follow an event source:
// WebSocket (events.go / session/events) and MQTT. The MQTT side is a
// PUBLISHER: the node pushes every state change to the broker named
// in the sender's IS-05 transport params, RETAINED, so a late joiner
// reads the current value the moment it subscribes — the broker plays
// the role the WebSocket's initial state message plays.
//
// Per the spec's MQTT transport doc the node also maintains a
// connection-status topic (retained {"message_type":
// "connection_status", "active": true|false}) tracking master_enable
// — that is what tells a consumer whether silence means "no changes"
// or "nobody is publishing".

package provider

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"sync"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/session/mqtt"
)

// mqttEventBinding is one MQTT event sender's live wiring.
type mqttEventBinding struct {
	senderID    string
	sourceID    string
	broker      string // host:port from the ACTIVE params
	topic       string
	statusTopic string
	active      bool
}

// mqttEventBridge publishes IS-07 state over MQTT for every event
// sender the bundle declares with transport urn:x-nmos:transport:mqtt.
type mqttEventBridge struct {
	logger *slog.Logger
	// stateMessage hands back the source's current state WITH flow_id
	// — the exact message a WebSocket subscriber would receive.
	stateMessage func(sourceID string) (any, bool)

	mu sync.Mutex
	// senderSource maps the bundle's MQTT event senders to their
	// sources — fixed at construction, the discriminator for the
	// activation hook.
	senderSource map[string]string
	bindings     map[string]*mqttEventBinding // by sender id
	clients      map[string]*mqtt.Client      // by broker host:port
	clientID     string
}

// newMQTTEventBridge scans the bundle; nil when no MQTT event sender
// exists (the common case — then nothing dials anywhere).
func newMQTTEventBridge(logger *slog.Logger, bundle *NodeConfig, stateMessage func(string) (any, bool)) *mqttEventBridge {
	if bundle == nil {
		return nil
	}
	senderSource := map[string]string{}
	for i := range bundle.Senders {
		snd := &bundle.Senders[i]
		if snd.Transport != is04.TransportMQTT || snd.FlowID == nil {
			continue
		}
		for j := range bundle.Flows {
			f := &bundle.Flows[j]
			if f.ID == *snd.FlowID && f.Format == formatData {
				senderSource[snd.ID] = f.SourceID
			}
		}
	}
	if len(senderSource) == 0 {
		return nil
	}
	return &mqttEventBridge{
		logger:       logger,
		stateMessage: stateMessage,
		senderSource: senderSource,
		bindings:     map[string]*mqttEventBinding{},
		clients:      map[string]*mqtt.Client{},
		clientID:     "dhs-node-" + bundle.Node.ID,
	}
}

// OnSenderActivation reflects one IS-05 activation. Called from the
// connection layer for every sender; non-MQTT-event senders return
// immediately.
func (b *mqttEventBridge) OnSenderActivation(id string, active is05.StagedSender) {
	if b == nil {
		return
	}
	sourceID, isEventSender := b.senderSource[id]
	if !isEventSender || len(active.TransportParams) == 0 {
		return
	}
	p := active.TransportParams[0]
	binding := &mqttEventBinding{
		senderID:    id,
		sourceID:    sourceID,
		broker:      mqttParamString(p, "destination_host") + ":" + paramPort(p, "destination_port"),
		topic:       mqttParamString(p, "broker_topic"),
		statusTopic: mqttParamString(p, "connection_status_broker_topic"),
		active:      active.MasterEnable,
	}
	b.mu.Lock()
	b.bindings[id] = binding
	b.mu.Unlock()

	client := b.clientFor(binding.broker)
	if client == nil {
		return
	}
	status, _ := json.Marshal(map[string]any{
		"message_type": "connection_status",
		"active":       binding.active,
	})
	if binding.statusTopic != "" {
		client.Publish(binding.statusTopic, status, true)
	}
	if binding.active && binding.topic != "" {
		if msg, found := b.stateMessage(sourceID); found {
			if payload, err := json.Marshal(msg); err == nil {
				client.Publish(binding.topic, payload, true)
			}
		}
	}
}

// OnStateChanged pushes a new state message to every active MQTT
// sender carrying this source.
func (b *mqttEventBridge) OnStateChanged(sourceID string, msg any) {
	if b == nil {
		return
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	b.mu.Lock()
	targets := make([]*mqttEventBinding, 0, 1)
	for _, binding := range b.bindings {
		if binding.sourceID == sourceID && binding.active && binding.topic != "" {
			targets = append(targets, binding)
		}
	}
	b.mu.Unlock()
	for _, binding := range targets {
		if client := b.clientFor(binding.broker); client != nil {
			client.Publish(binding.topic, payload, true)
		}
	}
}

// Close shuts every broker session down.
func (b *mqttEventBridge) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	clients := make([]*mqtt.Client, 0, len(b.clients))
	for _, c := range b.clients {
		clients = append(clients, c)
	}
	b.clients = map[string]*mqtt.Client{}
	b.mu.Unlock()
	for _, c := range clients {
		c.Close()
	}
}

// clientFor returns (starting on first use) the session for one broker.
func (b *mqttEventBridge) clientFor(broker string) *mqtt.Client {
	if broker == "" || broker == ":" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if c, have := b.clients[broker]; have {
		return c
	}
	c, err := mqtt.New(mqtt.Options{Addr: broker, ClientID: b.clientID, Logger: b.logger})
	if err != nil {
		if b.logger != nil {
			b.logger.Warn("is-07 mqtt: cannot start broker session",
				"plugin", "amwa", "api", "is-07", "broker", broker, "err", err)
		}
		return nil
	}
	b.clients[broker] = c
	return c
}

// mqttParamString reads one string transport param, "" when absent/null.
func mqttParamString(p is05.TransportParams, key string) string {
	if v, ok := p[key].(string); ok {
		return v
	}
	return ""
}

// paramPort renders a numeric transport param (json float64 or int).
func paramPort(p is05.TransportParams, key string) string {
	switch v := p[key].(type) {
	case float64:
		return strconv.Itoa(int(v))
	case int:
		return strconv.Itoa(v)
	}
	return ""
}
