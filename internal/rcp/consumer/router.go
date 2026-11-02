package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"dhs/internal/rcp/codec"
)

// Router names a router device: its name in Cerebrum's System View and
// its sub-device index. The RouteMaster is {"Cerebrum", 0}.
type Router struct {
	Device string
	Index  int
}

// RouteMaster is the router the RouteMaster itself is.
var RouteMaster = Router{Device: "Cerebrum", Index: 0}

func (r Router) path(tail string) string {
	return "/devices/" + url.PathEscape(r.Device) + "/" + strconv.Itoa(r.Index) + "/routers/" + tail
}

// Routes returns the routes table of a router. dest narrows it to one
// destination; 0 returns every destination.
func (c *Client) Routes(ctx context.Context, r Router, dest int64) (codec.Routes, error) {
	p := r.path("routes")
	if dest != 0 {
		p += "?DestinationId=" + strconv.FormatInt(dest, 10)
	}
	env, err := c.do(ctx, http.MethodGet, p, nil, true)
	if err != nil {
		return nil, err
	}
	routes := codec.Routes{}
	return routes, field(env, "routes", &routes)
}

// Take routes src to dest — on every level the destination exists on
// when level is 0, on that level only otherwise. The server answers
// before the route is made: read the table back.
func (c *Client) Take(ctx context.Context, r Router, dest, src int64, level int) error {
	if dest <= 0 {
		return errors.New("rcp: a destination id is required")
	}
	if src < 0 || level < 0 {
		return errors.New("rcp: a source id and a level cannot be negative")
	}
	_, err := c.do(ctx, http.MethodPatch, r.path("routes"), codec.TakeBody(dest, src, level), true)
	return err
}

// Mnemonics returns one of a router's mnemonic tables, keyed by id.
func (c *Client) Mnemonics(ctx context.Context, r Router, kind codec.MnemonicKind) (map[int64]codec.MnemonicEntry, error) {
	env, err := c.do(ctx, http.MethodGet, r.path(kind.Path()), nil, true)
	if err != nil {
		return nil, err
	}
	var raw map[string]codec.MnemonicEntry
	if err := field(env, kind.EnvelopeKey(), &raw); err != nil {
		return nil, err
	}
	out := make(map[int64]codec.MnemonicEntry, len(raw))
	for key, e := range raw {
		id, err := codec.KeyID(key)
		if err != nil {
			return nil, err
		}
		out[id] = e
	}
	return out, nil
}

// SetMnemonic writes one row of a mnemonic table.
//
// The document's body is the row itself. Cerebrum 2.5.3 answers "Error
// parsing request" to that and takes the row under its table key
// ("src_7"), which is the shape its own GET of a single row answers in —
// so that is the shape sent.
func (c *Client) SetMnemonic(ctx context.Context, r Router, kind codec.MnemonicKind, id int64, u codec.MnemonicUpdate) error {
	if id <= 0 {
		return errors.New("rcp: an id is required")
	}
	if u.Original == nil && len(u.Alternates) == 0 {
		return errors.New("rcp: a mnemonic update needs at least one field")
	}
	key := kind.KeyPrefix() + strconv.FormatInt(id, 10)
	body := map[string]json.RawMessage{}
	raw, err := json.Marshal(u)
	if err != nil {
		return fmt.Errorf("rcp: marshal mnemonic update: %w", err)
	}
	body[key] = raw
	_, err = c.do(ctx, http.MethodPatch, r.path(kind.Path()+"/"+strconv.FormatInt(id, 10)), body, true)
	return err
}

// Object is one value node of a device's object tree.
type Object struct {
	// Current is the value as the server sent it: a string, a number or
	// a boolean. An enum may come as its index or as a boolean, with the
	// names in AllowedValues.
	Current       any      `json:"current"`
	Writable      bool     `json:"writable"`
	AllowedValues []string `json:"allowedValues,omitempty"`
}

// ObjectAnswer is what a device object path holds: a value, the indices
// of a table, or neither.
type ObjectAnswer struct {
	// Value is nil when the path is not a value node. The server answers
	// an empty object for a group node and for a path that does not
	// exist alike, so nil is "no value here", not "no such path".
	Value   *Object
	Indices []string
	IsTable bool
}

func objectPath(device string, index int, path string) string {
	return "/devices/" + url.PathEscape(device) + "/" + strconv.Itoa(index) + "/object/" + url.PathEscape(path)
}

// GetObject reads one node of a device's object tree. path is the
// dotted path of the object browser ("License.License_Type"); index is
// the sub-device (slot), 0 for the device itself.
func (c *Client) GetObject(ctx context.Context, device string, index int, path string) (ObjectAnswer, error) {
	var out ObjectAnswer
	if device == "" || path == "" {
		return out, errors.New("rcp: a device and an object path are required")
	}
	env, err := c.do(ctx, http.MethodGet, objectPath(device, index, path), nil, true)
	if err != nil {
		return out, err
	}
	if raw, ok := env["indices"]; ok {
		out.IsTable = true
		out.Indices = []string{}
		return out, json.Unmarshal(raw, &out.Indices)
	}
	raw, ok := env["object"]
	if !ok {
		return out, nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return out, fmt.Errorf("rcp: decode \"object\": %w", err)
	}
	if _, has := probe["current"]; !has {
		return out, nil
	}
	out.Value = &Object{}
	return out, json.Unmarshal(raw, out.Value)
}

// SetObject writes one value. The value is always sent as a string, as
// the API document requires — for an enum, one of its allowed values.
func (c *Client) SetObject(ctx context.Context, device string, index int, path, value string) error {
	if device == "" || path == "" {
		return errors.New("rcp: a device and an object path are required")
	}
	_, err := c.do(ctx, http.MethodPut, objectPath(device, index, path), map[string]string{"value": value}, true)
	return err
}

// Devices returns the devices registered in Cerebrum, as the server
// describes them.
func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	env, err := c.do(ctx, http.MethodGet, "/devices", nil, true)
	if err != nil {
		return nil, err
	}
	var out []Device
	return out, field(env, "discoveryDevices", &out)
}

// Device is one registered device.
type Device struct {
	Name         string `json:"deviceName"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	IP           string `json:"deviceIP,omitempty"`
	SubDevices   []struct {
		Index int `json:"deviceIndex"`
	} `json:"subDevices,omitempty"`
	Resources map[string]json.RawMessage `json:"resources,omitempty"`
}
