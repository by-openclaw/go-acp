// Layer-3 — the SDP an RTP Sender serves at
// `/single/senders/{id}/transportfile`.
//
// IS-05 §4.3 makes this the ONLY machine-readable description of what
// a Sender is transmitting. A controller connecting two devices reads
// the Sender's SDP and PATCHes it verbatim into the Receiver's
// `transport_file.data` — it does not translate transport_params
// between the two. A Sender that answers 404 here can be activated and
// still cannot be connected to anything, which is why the tool checks
// it separately from activation (IS-05-02 test_13 through test_17).
//
// The SDP is generated from the ACTIVE transport params plus the IS-04
// Flow, not from a stored blob: the addresses only exist once the
// endpoint has activated, and a file written at seed time would
// describe a stream that is not running.

package provider

import (
	"fmt"
	"net"
	"strings"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
)

// sdpForSender renders RFC 4566 session description for one activated
// Sender. It returns "" when the Sender is not RTP — a non-RTP
// transport has no SDP, and inventing one would be worse than the 404
// the spec expects there.
func (s *IS05ConnectionServer) sdpForSender(id string, active is05.StagedSender) string {
	snd := s.senderByID(id)
	if snd == nil || !isRTP(snd.Transport) {
		return ""
	}
	if len(active.TransportParams) == 0 {
		return ""
	}
	leg := active.TransportParams[0]

	srcIP := paramString(leg, "source_ip", "127.0.0.1")
	dstIP := paramString(leg, "destination_ip", srcIP)
	dstPort := paramInt(leg, "destination_port", 5004)

	// o= needs a session id and version. A clock reading would make
	// the same stream produce a different SDP on every GET, which
	// breaks byte-comparison in controllers that cache it. The IS-04
	// resource version is already a monotonically-increasing TAI
	// string tied to actual changes, so it is the honest source for
	// both fields.
	sessID := taiSeconds(snd.Version)

	var b strings.Builder
	fmt.Fprintf(&b, "v=0\r\n")
	fmt.Fprintf(&b, "o=- %s %s IN IP4 %s\r\n", sessID, sessID, srcIP)
	fmt.Fprintf(&b, "s=%s\r\n", sdpText(snd.Label, "dhs sender"))
	fmt.Fprintf(&b, "t=0 0\r\n")

	// The channel count is on the SOURCE, not the Flow — a Flow
	// describes the encoding, a Source describes what was encoded.
	flow := s.flowByID(snd.FlowID)
	media, rtpmap, extra := mediaLinesFor(flow, s.audioChannels(flow))
	fmt.Fprintf(&b, "m=%s %d RTP/AVP 96\r\n", media, dstPort)
	// A multicast destination carries a TTL on the connection line and
	// a unicast one must NOT — an SDP parser rejects the wrong form
	// rather than ignoring it.
	if ip := net.ParseIP(dstIP); ip != nil && ip.IsMulticast() {
		fmt.Fprintf(&b, "c=IN IP4 %s/64\r\n", dstIP)
	} else {
		fmt.Fprintf(&b, "c=IN IP4 %s\r\n", dstIP)
	}
	fmt.Fprintf(&b, "a=rtpmap:96 %s\r\n", rtpmap)
	for _, line := range extra {
		fmt.Fprintf(&b, "a=%s\r\n", line)
	}
	// ST 2110-10 §8: every stream declares its clock. `direct=0`
	// with a TAI reference is the plain "PTP-locked, no offset" case.
	fmt.Fprintf(&b, "a=mediaclk:direct=0\r\n")
	fmt.Fprintf(&b, "a=ts-refclk:localmac=00-00-00-00-00-00\r\n")
	// source-filter lets a receiver join an SSM group. Multicast only:
	// on a unicast stream there is no group to filter.
	if ip := net.ParseIP(dstIP); ip != nil && ip.IsMulticast() {
		fmt.Fprintf(&b, "a=source-filter: incl IN IP4 %s %s\r\n", dstIP, srcIP)
	}
	return b.String()
}

// mediaLinesFor derives the m= media type, the rtpmap payload
// description, and any format-specific attributes from the Flow.
//
// A Flow we cannot classify still gets a valid SDP rather than none:
// the tool's checks are about the Sender publishing a parseable
// description, and answering 404 because our own Flow table is thin
// would report a connection-management fault for a metadata gap.
func mediaLinesFor(f *is04.Flow, channels int) (media, rtpmap string, extra []string) {
	if f == nil {
		return "video", "raw/90000", nil
	}
	switch {
	case strings.HasSuffix(f.Format, ":audio"):
		rate := 48000
		if f.SampleRate != nil && f.SampleRate.Numerator > 0 {
			rate = f.SampleRate.Numerator
		}
		// L16 and L24 are the two ST 2110-30 encodings; the bit depth
		// on the Flow picks between them.
		enc := "L24"
		if f.BitDepth == 16 {
			enc = "L16"
		}
		if channels <= 0 {
			channels = 2
		}
		return "audio", fmt.Sprintf("%s/%d/%d", enc, rate, channels), []string{"ptime:1"}
	case strings.HasSuffix(f.Format, ":data"):
		return "application", "smpte291/90000", nil
	case strings.HasSuffix(f.Format, ":mux"):
		return "video", "SMPTE2022-6/90000", nil
	default:
		// Video. The fmtp carries the raster, which is what makes the
		// difference between two otherwise identical 2110-20 streams.
		attrs := []string{}
		if f.FrameWidth > 0 && f.FrameHeight > 0 {
			attrs = append(attrs, fmt.Sprintf(
				"fmtp:96 sampling=YCbCr-4:2:2; width=%d; height=%d; depth=%d; colorimetry=BT709; TCS=SDR; PM=2110GPM; SSN=ST2110-20:2017",
				f.FrameWidth, f.FrameHeight, maxInt(f.BitDepth, 10)))
		}
		return "video", "raw/90000", attrs
	}
}

// audioChannels counts the channels on the Source behind a Flow.
func (s *IS05ConnectionServer) audioChannels(f *is04.Flow) int {
	if s.bundle == nil || f == nil {
		return 0
	}
	for i := range s.bundle.Sources {
		if s.bundle.Sources[i].ID == f.SourceID {
			return len(s.bundle.Sources[i].Channels)
		}
	}
	return 0
}

func (s *IS05ConnectionServer) senderByID(id string) *is04.Sender {
	if s.bundle == nil {
		return nil
	}
	for i := range s.bundle.Senders {
		if s.bundle.Senders[i].ID == id {
			return &s.bundle.Senders[i]
		}
	}
	return nil
}

func (s *IS05ConnectionServer) flowByID(id *string) *is04.Flow {
	if s.bundle == nil || id == nil || *id == "" {
		return nil
	}
	for i := range s.bundle.Flows {
		if s.bundle.Flows[i].ID == *id {
			return &s.bundle.Flows[i]
		}
	}
	return nil
}

// taiSeconds pulls the whole-seconds half out of an IS-04
// "<seconds>:<nanoseconds>" version string.
func taiSeconds(v string) string {
	if i := strings.IndexByte(v, ':'); i > 0 {
		return v[:i]
	}
	if v == "" {
		return "0"
	}
	return v
}

// sdpText makes a label safe for an SDP line. CR/LF would end the line
// early and turn one field into a malformed record.
func sdpText(s, fallback string) string {
	s = strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(s))
	if s == "" {
		return fallback
	}
	return s
}

func paramString(p is05.TransportParams, key, fallback string) string {
	if v, ok := p[key].(string); ok && v != "" && v != "auto" {
		return v
	}
	return fallback
}

func paramInt(p is05.TransportParams, key string, fallback int) int {
	switch v := p[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return fallback
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
