// Package configuration is the IS-14 Device Configuration API CLIENT —
// the half a Controller needs to read and set a Device's model
// (MS-05-02 over REST) and to back it up and restore it.
//
// The provider half (internal/amwa/provider/configuration.go) serves
// this API; nothing consumed it. Same layering as session/connection:
// HTTP mechanics only. The IS-14 payload shapes live in codec/is14 and
// the object-model vocabulary in codec/ms05.
package configuration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"dhs/internal/amwa/codec/is14"
	"dhs/internal/amwa/codec/ms05"
)

// Client speaks IS-14 to one Device's Configuration API.
//
// Base is the href IS-04 advertised in the Device's `controls` array
// under urn:x-nmos:control:configuration/vX.Y.
type Client struct {
	HTTP   *http.Client
	Base   string // e.g. "http://10.6.255.102:3000/x-nmos/configuration/v1.0"
	APIVer string
}

// NewClient builds a Client from an IS-14 control href; the version is
// read back out of the href so the two can never disagree.
func NewClient(controlHref string) (*Client, error) {
	base := strings.TrimRight(controlHref, "/")
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("nmos/configuration: parse %q: %w", controlHref, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("nmos/configuration: %q must be an absolute URL", controlHref)
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	ver := segs[len(segs)-1]
	if !strings.HasPrefix(ver, "v") {
		return nil, fmt.Errorf("nmos/configuration: %q does not end in an api_ver "+
			"(expected .../x-nmos/configuration/v1.0)", controlHref)
	}
	return &Client{
		HTTP:   &http.Client{Timeout: 30 * time.Second},
		Base:   base,
		APIVer: ver,
	}, nil
}

// Result is an MS-05 method result as IS-14 carries it: a status, and
// either a value or an error message. An IS-14 Device answers a failed
// get, set or invoke with a method result too (and an HTTP status that
// mirrors it), so a Result is returned for those as well — OK tells
// them apart.
type Result struct {
	Status       ms05.NcMethodStatus `json:"status"`
	Value        json.RawMessage     `json:"value,omitempty"`
	ErrorMessage string              `json:"errorMessage,omitempty"`
}

// OK reports a 2xx method status: 200, or the 298 / 299 a deprecated
// property or method answers with — done, with a note.
func (r Result) OK() bool { return r.Status >= 200 && r.Status < 300 }

// RolePaths lists every object of the Device model by its role path.
func (c *Client) RolePaths(ctx context.Context) ([]string, error) {
	return c.list(ctx, "rolePaths/")
}

// PropertyIDs lists the property ids ("1p1", "3p2", …) of one object.
func (c *Client) PropertyIDs(ctx context.Context, rolePath string) ([]string, error) {
	return c.list(ctx, "rolePaths/"+rolePath+"/properties/")
}

// MethodIDs lists the method ids ("1m1", "3m1", …) of one object.
func (c *Client) MethodIDs(ctx context.Context, rolePath string) ([]string, error) {
	return c.list(ctx, "rolePaths/"+rolePath+"/methods/")
}

// Descriptor reads an object's class descriptor (inherited members
// included); the Result's Value is the NcClassDescriptor.
func (c *Client) Descriptor(ctx context.Context, rolePath string) (Result, error) {
	return c.result(ctx, http.MethodGet, "rolePaths/"+rolePath+"/descriptor/", nil)
}

// PropertyDescriptor reads the datatype descriptor of one property.
func (c *Client) PropertyDescriptor(ctx context.Context, rolePath, propertyID string) (Result, error) {
	return c.result(ctx, http.MethodGet, "rolePaths/"+rolePath+"/properties/"+propertyID+"/descriptor/", nil)
}

// GetProperty reads one property's value.
func (c *Client) GetProperty(ctx context.Context, rolePath, propertyID string) (Result, error) {
	return c.result(ctx, http.MethodGet, "rolePaths/"+rolePath+"/properties/"+propertyID+"/value/", nil)
}

// SetProperty writes one property's value (any JSON the property's
// datatype takes).
func (c *Client) SetProperty(ctx context.Context, rolePath, propertyID string, value json.RawMessage) (Result, error) {
	body, _ := json.Marshal(is14.PropertyValuePutRequest{Value: value})
	return c.result(ctx, http.MethodPut, "rolePaths/"+rolePath+"/properties/"+propertyID+"/value/", body)
}

// Invoke calls one method with its arguments object (nil for none).
func (c *Client) Invoke(ctx context.Context, rolePath, methodID string, arguments json.RawMessage) (Result, error) {
	if arguments == nil {
		arguments = json.RawMessage(`{}`)
	}
	body, _ := json.Marshal(is14.MethodPatchRequest{Arguments: arguments})
	return c.result(ctx, http.MethodPatch, "rolePaths/"+rolePath+"/methods/"+methodID+"/", body)
}

// Backup reads the bulk properties of an object — and of everything
// under it with recurse — as the data set a restore takes back.
func (c *Client) Backup(ctx context.Context, rolePath string, recurse bool) (is14.BulkPropertiesHolder, Result, error) {
	res, err := c.result(ctx, http.MethodGet,
		fmt.Sprintf("rolePaths/%s/bulkProperties/?recurse=%t", rolePath, recurse), nil)
	if err != nil || !res.OK() {
		return is14.BulkPropertiesHolder{}, res, err
	}
	holder, err := is14.DecodeBulkPropertiesHolder(res.Value)
	return holder, res, err
}

// Validate asks the Device what a restore of the data set would do,
// object by object, without applying it.
func (c *Client) Validate(ctx context.Context, rolePath string, dataSet is14.BulkPropertiesHolder, recurse bool, mode is14.RestoreMode) ([]is14.ObjectPropertiesSetValidation, Result, error) {
	return c.bulkSet(ctx, http.MethodPatch, rolePath, dataSet, recurse, mode)
}

// Restore applies the data set and returns what happened to each
// object.
func (c *Client) Restore(ctx context.Context, rolePath string, dataSet is14.BulkPropertiesHolder, recurse bool, mode is14.RestoreMode) ([]is14.ObjectPropertiesSetValidation, Result, error) {
	return c.bulkSet(ctx, http.MethodPut, rolePath, dataSet, recurse, mode)
}

func (c *Client) bulkSet(ctx context.Context, method, rolePath string, dataSet is14.BulkPropertiesHolder, recurse bool, mode is14.RestoreMode) ([]is14.ObjectPropertiesSetValidation, Result, error) {
	if err := is14.ValidateRestoreMode(mode); err != nil {
		return nil, Result{}, err
	}
	body, _ := json.Marshal(is14.BulkPropertiesSetRequest{Arguments: &is14.BulkPropertiesSetArgs{
		DataSet: &dataSet, Recurse: &recurse, RestoreMode: &mode,
	}})
	res, err := c.result(ctx, method, "rolePaths/"+rolePath+"/bulkProperties/", body)
	if err != nil || !res.OK() {
		return nil, res, err
	}
	var validations []is14.ObjectPropertiesSetValidation
	if err := json.Unmarshal(res.Value, &validations); err != nil {
		return nil, res, fmt.Errorf("nmos/configuration: decode the restore validations: %w", err)
	}
	return validations, res, nil
}

// list reads an IS-14 index: an array of "<segment>/" entries, returned
// without their trailing slash.
func (c *Client) list(ctx context.Context, rest string) ([]string, error) {
	raw, code, err := c.do(ctx, http.MethodGet, rest, nil)
	if err != nil {
		return nil, err
	}
	if code < 200 || code >= 300 {
		return nil, &StatusError{What: rest, Code: code, Body: strings.TrimSpace(string(raw))}
	}
	var segs []string
	if err := json.Unmarshal(raw, &segs); err != nil {
		return nil, fmt.Errorf("nmos/configuration: decode %s: %w", rest, err)
	}
	for i, s := range segs {
		segs[i] = strings.TrimSuffix(s, "/")
	}
	return segs, nil
}

// result reads a method result. A non-2xx HTTP answer that carries one
// is the Device saying no in MS-05's vocabulary and is returned as a
// Result; one that does not is a StatusError.
func (c *Client) result(ctx context.Context, method, rest string, body []byte) (Result, error) {
	raw, code, err := c.do(ctx, method, rest, body)
	if err != nil {
		return Result{}, err
	}
	var res Result
	if err := json.Unmarshal(raw, &res); err != nil || res.Status == 0 {
		if code < 200 || code >= 300 {
			return Result{}, &StatusError{What: rest, Code: code, Body: strings.TrimSpace(string(raw))}
		}
		return Result{}, fmt.Errorf("nmos/configuration: %s: the answer is not a method result", rest)
	}
	return res, nil
}

func (c *Client) do(ctx context.Context, method, rest string, body []byte) ([]byte, int, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+"/"+rest, rd)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("nmos/configuration: %s: %w", rest, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20)) // a recursive backup of a large model is big
	return raw, resp.StatusCode, nil
}

// StatusError is a non-2xx answer that carried no method result, kept
// typed so a caller can tell a missing endpoint from a refusal.
type StatusError struct {
	What string
	Code int
	Body string
}

func (e *StatusError) Error() string {
	msg := e.Body
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	if msg == "" {
		return fmt.Sprintf("nmos/configuration: %s: HTTP %d", e.What, e.Code)
	}
	return fmt.Sprintf("nmos/configuration: %s: HTTP %d: %s", e.What, e.Code, msg)
}
