package codec

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The stream view: every io/ip sender and receiver, by UUID.
//
// Two shapes are in the field. CCM 0v1 §4.4 has a collection answer the
// array of its members' ids, each member being a resource of its own —
// SHUFFLE 6.0.0 does that. BRIDGE and CONVERT 7.0.3 answer the array of
// the members themselves (DecodeStreams). Which one a device speaks is
// read off its answer, never assumed.

// StreamCollection is one place streams live on a device.
type StreamCollection struct {
	// Path is the collection, API-relative: "/io/ip/senders/audio".
	Path    string
	Kind    Kind
	Essence Essence
}

// KnownStreamCollections is where BRIDGE and CONVERT keep their
// streams — what to read on a firmware that serves no api.yml to say.
var KnownStreamCollections = []StreamCollection{
	{"/io/ip/senders/video", KindSender, EssenceVideo},
	{"/io/ip/senders/audio", KindSender, EssenceAudio},
	{"/io/ip/senders/data", KindSender, EssenceData},
	{"/io/ip/receivers/video", KindReceiver, EssenceVideo},
	{"/io/ip/receivers/audio", KindReceiver, EssenceAudio},
	{"/io/ip/receivers/data", KindReceiver, EssenceData},
}

// StreamCollections is the stream collections a device's api.yml
// declares: every `/io/ip/senders/<essence>/{id}` and
// `/io/ip/receivers/<essence>/{id}` it has a GET for (§5.2).
func StreamCollections(spec *Spec) []StreamCollection {
	var out []StreamCollection
	for _, t := range spec.With(GET) {
		seg := strings.Split(strings.Trim(t, "/"), "/")
		if len(seg) != 5 || seg[0] != "io" || seg[1] != "ip" || !strings.HasPrefix(seg[4], "{") {
			continue
		}
		var kind Kind
		switch seg[2] {
		case "senders":
			kind = KindSender
		case "receivers":
			kind = KindReceiver
		default:
			continue
		}
		out = append(out, StreamCollection{"/" + strings.Join(seg[:4], "/"), kind, Essence(seg[3])})
	}
	return out
}

// StreamIDs reads a collection that answered the ids of its members
// (§4.4). ok is false when the body is anything else.
func StreamIDs(body []byte) (ids []string, ok bool) {
	if err := json.Unmarshal(body, &ids); err != nil {
		return nil, false
	}
	return ids, true
}

// memberStream is one stream as its own resource: the id is in the
// path and the two ST 2022-7 legs are named rather than listed.
type memberStream struct {
	Stream
	PrimaryLeg   *Leg `json:"primaryLeg"`
	SecondaryLeg *Leg `json:"secondaryLeg"`
}

// DecodeStream parses one member of a stream collection. id is the id
// the collection listed it under, and is the stream's UUID when the
// document carries none of its own.
func DecodeStream(id string, body []byte, kind Kind, essence Essence) (Stream, error) {
	var m memberStream
	if err := json.Unmarshal(body, &m); err != nil {
		return Stream{}, fmt.Errorf("neuron: decode %s/%s %s: %w", kind, essence, id, err)
	}
	s := m.Stream
	if s.UUID == "" {
		s.UUID = id
	}
	s.Kind, s.Essence = kind, essence
	if len(s.Legs) == 0 {
		for _, leg := range []*Leg{m.PrimaryLeg, m.SecondaryLeg} {
			if leg != nil {
				s.Legs = append(s.Legs, *leg)
			}
		}
	}
	return s, nil
}
