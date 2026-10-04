// Package control is the IS-12 Control Protocol CLIENT — the half a
// Controller needs to read and set a Device's model (MS-05-02) over the
// WebSocket IS-04 advertises under urn:x-nmos:control:ncp/vX.Y.
//
// The provider half (internal/amwa/provider/ncp.go) serves this
// protocol; nothing consumed it. Same layering as session/configuration:
// the socket and the pairing of a command with its response only. The
// frames live in codec/is12 and the object-model vocabulary in
// codec/ms05.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"dhs/internal/amwa/codec/is12"
	"dhs/internal/amwa/codec/ms05"
	httpsession "dhs/internal/amwa/session/http"
)

// ControlType is the device-control URN IS-12's "IS-04 interactions"
// names; the api_ver follows it.
const ControlType = "urn:x-nmos:control:ncp/"

// RootBlockOID is the oid MS-05-02 fixes for the root block.
const RootBlockOID = 1

// The MS-05-02 methods this client words itself: NcObject's Get and
// Set, NcBlock's GetMemberDescriptors, NcClassManager's GetControlClass.
var (
	MethodGet                  = is12.MethodID{Level: 1, Index: 1}
	MethodSet                  = is12.MethodID{Level: 1, Index: 2}
	MethodGetMemberDescriptors = is12.MethodID{Level: 2, Index: 1}
	MethodGetControlClass      = is12.MethodID{Level: 3, Index: 1}
)

// DefaultTimeout is how long a command waits for its response.
const DefaultTimeout = 10 * time.Second

// Client speaks IS-12 to one Device over one WebSocket. It is used from
// one goroutine: a command is sent and its response awaited before the
// next.
type Client struct {
	// Href is the control endpoint the socket was dialled at.
	Href string

	// Timeout bounds the wait for a response when the context carries no
	// deadline of its own.
	Timeout time.Duration

	ws     *httpsession.WebSocket
	handle int
	// queued are notifications that arrived while a command waited for
	// its response; Notifications delivers them first.
	queued []is12.Notification
}

// Dial opens the control WebSocket at href (the `ws://…/x-nmos/ncp/v1.0`
// of the Device's IS-04 controls).
func Dial(ctx context.Context, href string) (*Client, error) {
	ws, err := httpsession.DialWebSocket(ctx, href, nil)
	if err != nil {
		return nil, fmt.Errorf("nmos/control: %w", err)
	}
	return &Client{Href: href, Timeout: DefaultTimeout, ws: ws}, nil
}

// Close drops the socket.
func (c *Client) Close() error { return c.ws.Close() }

// OK reports a 2xx method status: 200, or the 298 / 299 a deprecated
// property or method answers with — done, with a note.
func OK(r is12.MethodResult) bool { return r.Status >= 200 && r.Status < 300 }

// Invoke calls one method of one object and returns the Device's method
// result. A Device that answers no does so in the result (OK tells);
// the error is for a socket that failed or a Device that did not speak
// the protocol.
func (c *Client) Invoke(ctx context.Context, oid int, method is12.MethodID, arguments any) (is12.MethodResult, error) {
	var none is12.MethodResult
	cmd := is12.Command{OID: oid, MethodID: method}
	if arguments != nil {
		raw, err := json.Marshal(arguments)
		if err != nil {
			return none, fmt.Errorf("nmos/control: arguments of %dm%d: %w", method.Level, method.Index, err)
		}
		cmd.Arguments = raw
	}
	// IS-12 handles are 1..65535; a long-lived socket goes round.
	c.handle = c.handle%65535 + 1
	cmd.Handle = c.handle
	frame, err := is12.Encode(is12.CommandMessage{MessageType: is12.MessageTypeCommand, Commands: []is12.Command{cmd}})
	if err != nil {
		return none, fmt.Errorf("nmos/control: %w", err)
	}
	var result *is12.MethodResult
	err = c.exchange(ctx, frame, func(m is12.Message) bool {
		resp, ok := m.(is12.CommandResponseMessage)
		if !ok {
			return false
		}
		for _, r := range resp.Responses {
			if r.Handle == cmd.Handle {
				result = &r.Result
				return true
			}
		}
		return false
	})
	if err != nil {
		return none, err
	}
	return *result, nil
}

// Get reads one property of one object.
func (c *Client) Get(ctx context.Context, oid int, id is12.PropertyID) (is12.MethodResult, error) {
	return c.Invoke(ctx, oid, MethodGet, map[string]any{"id": id})
}

// Set writes one property of one object.
func (c *Client) Set(ctx context.Context, oid int, id is12.PropertyID, value json.RawMessage) (is12.MethodResult, error) {
	return c.Invoke(ctx, oid, MethodSet, map[string]any{"id": id, "value": value})
}

// Members lists what a block holds; with recurse, everything under it.
func (c *Client) Members(ctx context.Context, blockOID int, recurse bool) ([]ms05.NcBlockMemberDescriptor, is12.MethodResult, error) {
	res, err := c.Invoke(ctx, blockOID, MethodGetMemberDescriptors, map[string]any{"recurse": recurse})
	if err != nil || !OK(res) {
		return nil, res, err
	}
	var members []ms05.NcBlockMemberDescriptor
	if err := json.Unmarshal(res.Value, &members); err != nil {
		return nil, res, fmt.Errorf("nmos/control: the members of block %d are not a list of member descriptors: %w", blockOID, err)
	}
	return members, res, nil
}

// Class asks a class manager for one class's descriptor, inherited
// members included.
func (c *Client) Class(ctx context.Context, managerOID int, classID ms05.NcClassId) (is12.MethodResult, error) {
	return c.Invoke(ctx, managerOID, MethodGetControlClass, map[string]any{"classId": classID, "includeInherited": true})
}

// Subscribe asks for the property-changed notifications of these
// objects — the whole set, replacing any earlier one — and returns the
// oids the Device took.
func (c *Client) Subscribe(ctx context.Context, oids []int) ([]int, error) {
	if oids == nil {
		oids = []int{}
	}
	// The one thing the codec refuses in a subscription is a missing list.
	frame, _ := is12.Encode(is12.SubscriptionMessage{MessageType: is12.MessageTypeSubscription, Subscriptions: oids})
	var taken []int
	err := c.exchange(ctx, frame, func(m is12.Message) bool {
		resp, ok := m.(is12.SubscriptionResponseMessage)
		if ok {
			taken = resp.Subscriptions
		}
		return ok
	})
	return taken, err
}

// Notifications hands every notification to fn until ctx ends, which is
// the ordinary way out and returns nil.
func (c *Client) Notifications(ctx context.Context, fn func(is12.Notification)) error {
	for _, n := range c.queued {
		fn(n)
	}
	c.queued = nil

	// A read cannot be given a context: its deadline is brought forward
	// when the context ends.
	_ = c.ws.SetReadDeadline(time.Time{})
	released := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = c.ws.SetReadDeadline(time.Now())
		close(released)
	})
	// Once the release has started it is waited for: left running, it
	// would cut short whatever reads this socket next.
	defer func() {
		if !stop() {
			<-released
		}
	}()
	for {
		if ctx.Err() != nil {
			return nil
		}
		raw, err := c.ws.ReadText()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("nmos/control: read: %w", err)
		}
		m, err := is12.Decode(raw)
		if err != nil {
			return fmt.Errorf("nmos/control: the Device sent a frame that is not IS-12: %w", err)
		}
		if note, ok := m.(is12.NotificationMessage); ok {
			for _, n := range note.Notifications {
				fn(n)
			}
		}
	}
}

// ProtocolError is the Device's own refusal of a frame (an IS-12 Error
// message): the frame was not a command it could take at all.
type ProtocolError struct {
	Status  int
	Message string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("nmos/control: the Device refused the message with status %d: %s", e.Status, e.Message)
}

// exchange sends one frame and reads until done reports the answer.
// Notifications met on the way are kept for Notifications.
func (c *Client) exchange(ctx context.Context, frame []byte, done func(is12.Message) bool) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(c.Timeout)
	}
	if err := c.ws.SendText(frame); err != nil {
		return fmt.Errorf("nmos/control: send: %w", err)
	}
	_ = c.ws.SetReadDeadline(deadline)
	for {
		raw, err := c.ws.ReadText()
		if err != nil {
			if errors.Is(err, httpsession.ErrWebSocketClosed) {
				return errors.New("nmos/control: the Device closed the socket before it answered")
			}
			return fmt.Errorf("nmos/control: no answer from the Device: %w", err)
		}
		m, err := is12.Decode(raw)
		if err != nil {
			return fmt.Errorf("nmos/control: the Device answered with a frame that is not IS-12: %w", err)
		}
		switch v := m.(type) {
		case is12.NotificationMessage:
			c.queued = append(c.queued, v.Notifications...)
		case is12.ErrorMessage:
			return &ProtocolError{Status: v.Status, Message: v.ErrorMessage}
		default:
			if done(m) {
				return nil
			}
		}
	}
}
