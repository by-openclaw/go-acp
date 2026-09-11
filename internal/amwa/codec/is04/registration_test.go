package is04

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestEncodeRegistrationVersionedFallsBackWithoutCodec: a nil codec
// means "canonical shape", byte-for-byte what EncodeRegistration emits.
func TestEncodeRegistrationVersionedFallsBackWithoutCodec(t *testing.T) {
	n := validNode()
	want, err := EncodeRegistration(ResourceNode, &n)
	if err != nil {
		t.Fatalf("canonical encode: %v", err)
	}
	got, err := EncodeRegistrationVersioned(nil, ResourceNode, &n)
	if err != nil {
		t.Fatalf("versioned encode with nil codec: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("nil codec must produce the canonical envelope\n got %s\nwant %s", got, want)
	}
}

// TestEncodeRegistrationVersionedWrapsEveryResource: whatever the
// codec renders is what the envelope carries, for each of the six
// resource types, under the singular type name the Registration API
// expects.
func TestEncodeRegistrationVersionedWrapsEveryResource(t *testing.T) {
	c := stubCodec{specID: SpecID, apiVer: "v1.3", encoded: `{"stub":true}`}
	cases := []struct {
		typ  ResourceType
		data any
	}{
		{ResourceNode, &Node{}},
		{ResourceDevice, &Device{}},
		{ResourceSource, &Source{}},
		{ResourceFlow, &Flow{}},
		{ResourceSender, &Sender{}},
		{ResourceReceiver, &Receiver{}},
	}
	for _, tc := range cases {
		t.Run(string(tc.typ), func(t *testing.T) {
			body, err := EncodeRegistrationVersioned(c, tc.typ, tc.data)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			r, err := DecodeRegistration(body)
			if err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if r.Type != tc.typ {
				t.Errorf("type = %q, want %q", r.Type, tc.typ)
			}
			if string(r.Data) != c.encoded {
				t.Errorf("data = %s, want the codec's bytes %s", r.Data, c.encoded)
			}
		})
	}
}

// TestEncodeRegistrationVersionedRefusesMismatchedData: the type tag
// and the Go value must agree; the error names both so the caller can
// see which side is wrong.
func TestEncodeRegistrationVersionedRefusesMismatchedData(t *testing.T) {
	c := stubCodec{specID: SpecID, apiVer: "v1.3", encoded: `{}`}
	cases := []struct {
		typ  ResourceType
		data any
		want string
	}{
		{ResourceNode, &Device{}, "want *Node"},
		{ResourceDevice, &Node{}, "want *Device"},
		{ResourceSource, &Node{}, "want *Source"},
		{ResourceFlow, &Node{}, "want *Flow"},
		{ResourceSender, &Node{}, "want *Sender"},
		{ResourceReceiver, &Node{}, "want *Receiver"},
	}
	for _, tc := range cases {
		t.Run(string(tc.typ), func(t *testing.T) {
			_, err := EncodeRegistrationVersioned(c, tc.typ, tc.data)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error saying %q, got %v", tc.want, err)
			}
		})
	}
}

func TestEncodeRegistrationVersionedRefusesUnknownType(t *testing.T) {
	c := stubCodec{specID: SpecID, apiVer: "v1.3", encoded: `{}`}
	_, err := EncodeRegistrationVersioned(c, ResourceType("bogus"), &Node{})
	if err == nil || !strings.Contains(err.Error(), `"bogus"`) {
		t.Fatalf("want the invalid type named, got %v", err)
	}
}

// TestEncodeRegistrationVersionedSurfacesCodecFailure: a codec that
// refuses the resource (its minor's schema rejected it) fails the
// registration with that cause wrapped, not with a generic message.
func TestEncodeRegistrationVersionedSurfacesCodecFailure(t *testing.T) {
	boom := errors.New("v1.0.3 schema rejected it")
	c := stubCodec{specID: SpecID, apiVer: "v1.0", encodeErr: boom}
	_, err := EncodeRegistrationVersioned(c, ResourceNode, &Node{})
	if !errors.Is(err, boom) {
		t.Fatalf("codec failure must be wrapped with %%w, got %v", err)
	}
	if !strings.Contains(err.Error(), "codec encode node") {
		t.Fatalf("error must say which stage failed, got %v", err)
	}
}

// TestEncodeRegistrationVersionedRefusesCodecGarbage: bytes that are
// not JSON cannot be put on the wire inside a JSON envelope.
func TestEncodeRegistrationVersionedRefusesCodecGarbage(t *testing.T) {
	c := stubCodec{specID: SpecID, apiVer: "v1.3", encoded: `{not json`}
	_, err := EncodeRegistrationVersioned(c, ResourceSender, &Sender{})
	if err == nil || !strings.Contains(err.Error(), "registration envelope") {
		t.Fatalf("want an envelope error, got %v", err)
	}
}

func TestEncodeRegistrationRefusesUnmarshallableData(t *testing.T) {
	_, err := EncodeRegistration(ResourceNode, make(chan int))
	if err == nil || !strings.Contains(err.Error(), "marshal node") {
		t.Fatalf("want a marshal error naming the resource, got %v", err)
	}
}

// TestEncodeRegistrationSurfacesEnvelopeFailure exercises the one
// branch no input can reach (see marshalEnvelope): the failure must be
// wrapped and returned, not dropped.
func TestEncodeRegistrationSurfacesEnvelopeFailure(t *testing.T) {
	boom := errors.New("envelope refused")
	old := marshalEnvelope
	marshalEnvelope = func(any) ([]byte, error) { return nil, boom }
	t.Cleanup(func() { marshalEnvelope = old })

	n := validNode()
	_, err := EncodeRegistration(ResourceNode, &n)
	if !errors.Is(err, boom) {
		t.Fatalf("envelope failure must be wrapped with %%w, got %v", err)
	}
	if !strings.Contains(err.Error(), "registration envelope") {
		t.Fatalf("error must name the envelope stage, got %v", err)
	}
}

// TestDecodeRegistrationIsStrict: the envelope is OUR wire format
// between Node and Registry, so unlike a peer's resource body it is
// read strictly. Broken JSON and an unknown envelope key are errors.
func TestDecodeRegistrationIsStrict(t *testing.T) {
	cases := map[string]string{
		"broken json":          `{"type":`,
		"unknown envelope key": `{"type":"node","data":{},"extra":1}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeRegistration([]byte(body))
			if err == nil || !strings.Contains(err.Error(), "decode registration") {
				t.Fatalf("want a decode error, got %v", err)
			}
		})
	}
}

// TestHealthResponseShape pins the `{ "health": "<unix-seconds>" }`
// body the Registration API returns on heartbeat.
func TestHealthResponseShape(t *testing.T) {
	raw, err := json.Marshal(HealthResponse{Health: "1700000000"})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"health":"1700000000"}` {
		t.Fatalf("health body = %s", raw)
	}
}
