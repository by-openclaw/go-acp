package codec

import (
	"reflect"
	"testing"
)

// The collections each product's own api.yml declares.
func TestStreamCollectionsAreReadFromTheSpec(t *testing.T) {
	shuffle, err := ParseSpec(read(t, "SHUFFLE@2.0.0-openapi.yml"))
	if err != nil {
		t.Fatal(err)
	}
	// An audio shuffler has audio, and nothing else to ask for.
	if got := StreamCollections(shuffle); !reflect.DeepEqual(got, []StreamCollection{
		{"/io/ip/receivers/audio", KindReceiver, EssenceAudio},
		{"/io/ip/senders/audio", KindSender, EssenceAudio},
	}) {
		t.Errorf("SHUFFLE = %+v", got)
	}

	bridge, err := ParseSpec(read(t, "BRIDGE@7.0.3-api.yml"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, c := range StreamCollections(bridge) {
		got[c.Path] = true
	}
	want := map[string]bool{}
	for _, c := range KnownStreamCollections {
		want[c.Path] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BRIDGE declares %v, the known six are %v", got, want)
	}

	// Neither a stream below something else nor another io/ip family.
	odd, err := ParseSpec([]byte("openapi: 3.1.1\npaths:\n  /io/ip/monitors/audio/{uuid}:\n    get:\n      operationId: A\n  /io/sdi/inputs/x/{uuid}:\n    get:\n      operationId: B\n  /io/ip/senders/audio/{uuid}/status:\n    get:\n      operationId: C\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := StreamCollections(odd); got != nil {
		t.Errorf("not stream collections: %+v", got)
	}
}

func TestStreamIDsReadsAnIDListAndNothingElse(t *testing.T) {
	// SHUFFLE 6.0.0, GET /io/ip/senders/audio (first two of 1544).
	ids, ok := StreamIDs([]byte(`["1a8a423b-b8cc-5e7d-a350-f928731c4792","7829024d-243d-5d6b-a0e1-e01bf2c2bcf7"]`))
	if !ok || !reflect.DeepEqual(ids, []string{"1a8a423b-b8cc-5e7d-a350-f928731c4792", "7829024d-243d-5d6b-a0e1-e01bf2c2bcf7"}) {
		t.Errorf("ids = %q, %v", ids, ok)
	}
	// BRIDGE answers the streams themselves.
	if _, ok := StreamIDs(read(t, "senders-video.json")); ok {
		t.Error("an array of streams was taken for an id list")
	}
}

func TestDecodeStreamReadsAMemberAsTheShuffleServesIt(t *testing.T) {
	// SHUFFLE 6.0.0 (10.6.255.103, 2026-10-02),
	// GET /io/ip/senders/audio/1a8a423b-b8cc-5e7d-a350-f928731c4792.
	body := []byte(`{"bitDepth":"24","enable":true,"inputSelection":"Main","mediaType":"s2110-30","name":"Output Audio Stream 1","nmos":{"groupHint":"","label":""},"nrChannels":"8","packetTime":"125us","primaryLeg":{"ip":"239.30.1.1","mac":"a6385727-40c6-57b0-b435-2acd7504e3c2","port":30000},"secondaryLeg":{"ip":"0.0.0.0","mac":"c76b648b-f00b-5cdd-b784-31083dabffc2","port":12700}}`)
	s, err := DecodeStream("1a8a423b-b8cc-5e7d-a350-f928731c4792", body, KindSender, EssenceAudio)
	if err != nil {
		t.Fatal(err)
	}
	if s.UUID != "1a8a423b-b8cc-5e7d-a350-f928731c4792" || s.Name != "Output Audio Stream 1" || !s.Enable ||
		s.Kind != KindSender || s.Essence != EssenceAudio || s.MediaType != "s2110-30" {
		t.Errorf("stream = %+v", s)
	}
	want := []Leg{
		{IP: "239.30.1.1", Port: 30000, StreamID: "a6385727-40c6-57b0-b435-2acd7504e3c2"},
		{IP: "0.0.0.0", Port: 12700, StreamID: "c76b648b-f00b-5cdd-b784-31083dabffc2"},
	}
	if !reflect.DeepEqual(s.Legs, want) {
		t.Errorf("legs = %+v", s.Legs)
	}

	// A receiver names one leg only, and a member that lists its legs
	// and carries its own uuid keeps both.
	s, err = DecodeStream("listed-id", []byte(`{"uuid":"own-id","name":"n","legs":[{"ip":"1.1.1.1","port":1,"mac":"m"}],"primaryLeg":{"ip":"9.9.9.9"}}`), KindReceiver, EssenceVideo)
	if err != nil || s.UUID != "own-id" || len(s.Legs) != 1 || s.Legs[0].IP != "1.1.1.1" {
		t.Errorf("stream = %+v, %v", s, err)
	}
	s, err = DecodeStream("r1", []byte(`{"name":"rx","primaryLeg":{"mac":"m1"}}`), KindReceiver, EssenceAudio)
	if err != nil || len(s.Legs) != 1 || s.Legs[0].StreamID != "m1" {
		t.Errorf("stream = %+v, %v", s, err)
	}
	if _, err := DecodeStream("x", []byte(`[`), KindSender, EssenceAudio); err == nil {
		t.Error("an unreadable member was decoded")
	}
}
