package consumer

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

// checkPlant is a snapshot built in memory — the check reads only what
// a walk produced, so it needs no server.
func checkPlant() *CatalogueSnapshot {
	flowID, audioFlow := uuidN(31), uuidN(32)
	video := senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)
	video.FlowID = &flowID
	audio := senderOn(uuidN(5), "MIC 1", testUUID, is04.TransportRTPMcast)
	audio.FlowID = &audioFlow
	idle := senderOn(uuidN(6), "IDLE", testUUID, is04.TransportRTPMcast)
	idle.FlowID = nil
	orphanFlow := uuidN(99)
	orphan := senderOn(uuidN(7), "ORPHAN", testUUID, is04.TransportRTPMcast)
	orphan.FlowID = &orphanFlow

	return &CatalogueSnapshot{
		Sources: []is04.Source{{
			ResourceCore: is04.ResourceCore{ID: uuidN(42)},
			Channels:     []is04.SourceAudioChannel{{Label: "L"}, {Label: "R"}},
		}},
		Flows: []is04.Flow{
			{ResourceCore: is04.ResourceCore{ID: flowID}, SourceID: uuidN(41), Format: is04.FormatVideo,
				MediaType: "video/raw", FrameWidth: 3840, FrameHeight: 2160, GrainRate: &is04.GrainRate{Numerator: 50}},
			{ResourceCore: is04.ResourceCore{ID: audioFlow}, SourceID: uuidN(42), Format: is04.FormatAudio,
				MediaType: "audio/L24", BitDepth: 24, SampleRate: &is04.GrainRate{Numerator: 48000}},
		},
		Senders:   []is04.Sender{video, audio, idle, orphan},
		Receivers: []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportRTP)},
	}
}

func constraintSets(t *testing.T, src string) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(src), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// What refuses a route, and what does not: only something the Receiver
// declares and IS-04 states about the stream.
func TestRouteProblems(t *testing.T) {
	const (
		videoTx, audioTx, idleTx, orphanTx = 1, 5, 6, 7
	)
	cases := []struct {
		name     string
		sender   int
		receiver func(r *is04.Receiver)
		want     []string // substrings, one per expected problem; nil = no problem
	}{
		{"a raw video sender on a raw video receiver", videoTx, nil, nil},
		{"a receiver that declares nothing takes anything", audioTx,
			func(r *is04.Receiver) { r.Format = ""; r.Caps = is04.ReceiverCaps{} }, nil},
		{"a sender nobody has heard of", 77, nil, []string{"is not in the catalogue"}},
		{"an audio sender on a video receiver", audioTx, nil,
			[]string{"the receiver is video, the flow is audio", "the flow is audio/L24, the receiver takes video/raw"}},
		{"a media type outside the receiver's list", videoTx,
			func(r *is04.Receiver) { r.Caps.MediaTypes = []string{"video/jxsv", "video/H264"} },
			[]string{"the flow is video/raw, the receiver takes video/jxsv, video/H264"}},
		{"media types compare without case", videoTx,
			func(r *is04.Receiver) { r.Caps.MediaTypes = []string{"VIDEO/RAW"} }, nil},
		{"a transport the receiver does not speak", videoTx,
			func(r *is04.Receiver) { r.Transport = is04.TransportMXL },
			[]string{"the receiver takes mxl, the sender emits rtp.mcast"}},
		{"a unicast-only receiver and a multicast sender", videoTx,
			func(r *is04.Receiver) { r.Transport = "urn:x-nmos:transport:rtp.ucast" },
			[]string{"the receiver takes rtp.ucast, the sender emits rtp.mcast"}},
		{"a stream past every constraint set", videoTx,
			func(r *is04.Receiver) {
				r.Caps.ConstraintSets = constraintSets(t, `[
					{"urn:x-nmos:cap:meta:label":"HD","urn:x-nmos:cap:format:frame_height":{"maximum":1080}},
					{"urn:x-nmos:cap:format:grain_rate":{"enum":[{"numerator":60000,"denominator":1001}]}}]`)
			},
			[]string{"no constraint set of the receiver admits the stream — HD: format:frame_height is 2160, outside maximum 1080; constraint set 1: format:grain_rate is 50"}},
		{"a stream one constraint set admits", videoTx,
			func(r *is04.Receiver) {
				r.Caps.ConstraintSets = constraintSets(t, `[
					{"urn:x-nmos:cap:format:frame_height":{"maximum":1080}},
					{"urn:x-nmos:cap:format:frame_height":{"enum":[2160]},"urn:x-nmos:cap:transport:packet_time":{"enum":[1]}}]`)
			}, nil},
		{"audio judged with the source's channel count", audioTx,
			func(r *is04.Receiver) {
				r.Format = is04.FormatAudio
				r.Caps = is04.ReceiverCaps{MediaTypes: []string{"audio/L24"},
					ConstraintSets: constraintSets(t, `[{"urn:x-nmos:cap:format:channel_count":{"minimum":8}}]`)}
			},
			[]string{"format:channel_count is 2, outside minimum 8"}},
		{"a sender with no flow has no stream to compare", idleTx,
			func(r *is04.Receiver) { r.Caps.MediaTypes = []string{"video/jxsv"} }, nil},
		{"a sender whose flow the catalogue lacks is not judged on it", orphanTx,
			func(r *is04.Receiver) { r.Caps.MediaTypes = []string{"video/jxsv"} }, nil},
	}
	for _, tc := range cases {
		snap := checkPlant()
		if tc.receiver != nil {
			tc.receiver(&snap.Receivers[0])
		}
		got := routeProblems(snap, snap, uuidN(tc.sender), uuidN(2))
		if len(got) != len(tc.want) {
			t.Errorf("%s: problems = %q, want %d", tc.name, got, len(tc.want))
			continue
		}
		for i, want := range tc.want {
			if !strings.Contains(got[i], want) {
				t.Errorf("%s: problem %d = %q, want it to say %q", tc.name, i, got[i], want)
			}
		}
	}
	// An unknown Receiver is resolveRoute's to report, not the check's.
	if got := routeProblems(checkPlant(), checkPlant(), uuidN(1), uuidN(404)); got != nil {
		t.Errorf("an unknown receiver = %q, want the check silent", got)
	}
}

func TestTransportsCompatible(t *testing.T) {
	const rtp, mcast, ucast = "urn:x-nmos:transport:rtp", "urn:x-nmos:transport:rtp.mcast", "urn:x-nmos:transport:rtp.ucast"
	cases := []struct {
		receiver, sender string
		want             bool
	}{
		{rtp, rtp, true}, {rtp, mcast, true}, {rtp, ucast, true},
		{mcast, rtp, true}, {mcast, mcast, true}, {mcast, ucast, false},
		{rtp, "urn:x-nmos:transport:mxl", false},
		{"urn:x-nmos:transport:mxl", "urn:x-nmos:transport:mxl.shm", true},
		{"urn:x-vendor:transport:srt", "urn:x-vendor:transport:srt", true},
		{"urn:x-vendor:transport:srt", rtp, false},
	}
	for _, tc := range cases {
		if got := transportsCompatible(tc.receiver, tc.sender); got != tc.want {
			t.Errorf("receiver %s, sender %s = %v, want %v", tc.receiver, tc.sender, got, tc.want)
		}
	}
	if got := shortURN("urn:x-vendor:thing"); got != "urn:x-vendor:thing" {
		t.Errorf("a URN outside x-nmos is shortened to %q", got)
	}
}

// Connect refuses a route the Receiver cannot decode, with the reason
// and before anything is sent; --force sends it and puts the override
// on the record. A salvo with one such route is refused whole.
func TestConnectRefusesWhatTheReceiverCannotTake(t *testing.T) {
	const sdp = "v=0\r\no=- 0 0 IN IP4 10.6.0.9\r\n"
	build := func() (*harness, *int) {
		h := newHarness(t)
		flowID := uuidN(31)
		tx := senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)
		tx.FlowID = &flowID
		rx := receiverOn(uuidN(2), testUUID, is04.TransportRTP)
		rx.Caps.MediaTypes = []string{"video/jxsv"}
		h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
		h.cat.senders = []is04.Sender{tx}
		h.cat.receivers = []is04.Receiver{rx, receiverOn(uuidN(3), testUUID, is04.TransportRTP)}
		h.cat.flows = []is04.Flow{{
			ResourceCore: is04.ResourceCore{ID: flowID, Version: "0:0", Label: "f", Description: "f", Tags: map[string][]string{}},
			SourceID:     uuidN(41), DeviceID: testUUID, Parents: []string{},
			Format: is04.FormatVideo, MediaType: "video/raw",
			FrameWidth: 1920, FrameHeight: 1080, ColorSpace: "BT709", Interlace: "progressive",
			Components: []is04.FlowVideoComponent{{Name: "Y", Width: 1920, Height: 1080, BitDepth: 10}},
		}}
		sent := 0
		inner := connectIS05(t, sdp)
		h.is05 = func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				sent++
			}
			inner(w, r)
		}
		return h, &sent
	}

	h, sent := build()
	_, err := h.ctrl.Connect(context.Background(), ConnectRequest{SenderID: uuidN(1), ReceiverID: uuidN(2)})
	if err == nil || !strings.Contains(err.Error(), "cannot take sender") ||
		!strings.Contains(err.Error(), "the flow is video/raw, the receiver takes video/jxsv") ||
		!strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want the refusal with its reason", err)
	}
	if *sent != 0 {
		t.Errorf("%d requests were sent for a refused route", *sent)
	}

	// --force: sent, and recorded.
	res, err := h.ctrl.Connect(context.Background(), ConnectRequest{SenderID: uuidN(1), ReceiverID: uuidN(2), Force: true})
	if err != nil || !res.MasterEnable || *sent != 1 {
		t.Fatalf("forced connect = %+v, %v (sent %d)", res, err, *sent)
	}
	if fired := codesFired(h.rep); fired["nmos_bcp00401_route_forced"] != 1 {
		t.Errorf("events = %v, want the override recorded once", fired)
	}

	// A salvo: one good route, one the receiver cannot take — nothing goes out.
	h, sent = build()
	_, err = h.ctrl.ConnectBulk(context.Background(), []ConnectRequest{
		{SenderID: uuidN(1), ReceiverID: uuidN(3)},
		{SenderID: uuidN(1), ReceiverID: uuidN(2)},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "route 2:") || !strings.Contains(err.Error(), "cannot take sender") {
		t.Errorf("salvo err = %v, want route 2 refused", err)
	}
	if *sent != 0 {
		t.Errorf("%d requests were sent for a salvo refused whole", *sent)
	}

	// A disconnect names no sender and is never checked.
	if _, err := h.ctrl.Connect(context.Background(), ConnectRequest{ReceiverID: uuidN(2)}); err != nil {
		t.Errorf("disconnect refused: %v", err)
	}
}
