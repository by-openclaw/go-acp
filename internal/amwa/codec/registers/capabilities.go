package registers

// The AMWA NMOS Capabilities register (v1.0), the cap:format:*,
// cap:transport:* and cap:meta:* parameter constraints BCP-004-01
// constraint sets are built from. Values are transcribed from the
// register text at https://specs.amwa.tv/nmos-parameter-registers/
// branches/main/capabilities/ — never from our own encoder.
//
// Kinds: enum values are the register's listed options; numeric
// parameters carry the register's bound where it states one and stay
// open otherwise; grain_rate / sample_rate style rationals are
// KindRational.

func f(v float64) *float64 { return &v }

func init() {
	register(Register{
		Name:    "capabilities",
		Version: "v1.0",
		Params: []Param{
			// ---- cap:format:* ----
			{URN: "urn:x-nmos:cap:format:media_type", Name: "Media Type", Kind: KindEnum,
				Description: "RTP payload media type of the Flow",
				Values: []string{
					"video/raw", "video/jxsv", "video/smpte291", "video/SMPTE2022-6",
					"video/H264", "video/H265", "audio/L16", "audio/L24", "audio/L32",
					"application/mp2t",
				}},
			{URN: "urn:x-nmos:cap:format:grain_rate", Name: "Grain Rate", Kind: KindRational,
				Description: "Grain rate as an NMOS rational (numerator/denominator)"},
			{URN: "urn:x-nmos:cap:format:frame_width", Name: "Frame Width", Kind: KindInteger,
				Description: "Video frame width in pixels", Min: f(1)},
			{URN: "urn:x-nmos:cap:format:frame_height", Name: "Frame Height", Kind: KindInteger,
				Description: "Video frame height in pixels", Min: f(1)},
			{URN: "urn:x-nmos:cap:format:interlace_mode", Name: "Interlace Mode", Kind: KindEnum,
				Values: []string{"progressive", "interlaced_tff", "interlaced_bff", "interlaced_psf"}},
			{URN: "urn:x-nmos:cap:format:colorspace", Name: "Colorspace", Kind: KindEnum,
				Values: []string{"BT601", "BT709", "BT2020", "BT2100", "ST2065-1", "ST2065-3"}},
			{URN: "urn:x-nmos:cap:format:transfer_characteristic", Name: "Transfer Characteristic", Kind: KindEnum,
				Values: []string{"SDR", "HLG", "PQ", "ST2065-1", "unspecified"}},
			{URN: "urn:x-nmos:cap:format:color_sampling", Name: "Color (Chroma) Sampling", Kind: KindEnum,
				Values: []string{"YCbCr-4:4:4", "YCbCr-4:2:2", "YCbCr-4:2:0", "RGB", "RGBA", "ICtCp-4:4:4", "XYZ", "KEY"}},
			{URN: "urn:x-nmos:cap:format:component_depth", Name: "Component Depth", Kind: KindInteger,
				Description: "Bits per component", Min: f(8), Max: f(16)},
			{URN: "urn:x-nmos:cap:format:channel_count", Name: "Channel Count", Kind: KindInteger,
				Description: "Audio channel count", Min: f(1)},
			{URN: "urn:x-nmos:cap:format:sample_rate", Name: "Sample Rate", Kind: KindRational,
				Description: "Audio sample rate as an NMOS rational"},
			{URN: "urn:x-nmos:cap:format:sample_depth", Name: "Sample Depth", Kind: KindInteger,
				Description: "Audio bits per sample", Min: f(8)},
			{URN: "urn:x-nmos:cap:format:event_type", Name: "Event Type", Kind: KindString,
				Description: "IS-07 event type the Flow carries"},

			// ---- cap:transport:* ----
			{URN: "urn:x-nmos:cap:transport:packet_time", Name: "Packet Time", Kind: KindNumber,
				Description: "RTP packet time in milliseconds", Min: f(0)},
			{URN: "urn:x-nmos:cap:transport:max_packet_time", Name: "Max Packet Time", Kind: KindNumber,
				Min: f(0)},
			{URN: "urn:x-nmos:cap:transport:st2110_21_sender_type", Name: "ST 2110-21 Sender Type", Kind: KindEnum,
				Values: []string{"2110TPN", "2110TPNL", "2110TPW"}},
			{URN: "urn:x-nmos:cap:transport:bit_rate", Name: "Bit Rate", Kind: KindInteger,
				Description: "Transport bit rate in kbit/s", Min: f(0)},
			{URN: "urn:x-nmos:cap:transport:packet_transmission_mode", Name: "Packet Transmission Mode", Kind: KindEnum,
				Values: []string{"codestream", "slice_sequential", "slice_out_of_order"}},

			// ---- cap:meta:* ----
			{URN: "urn:x-nmos:cap:meta:label", Name: "Constraint Set Label", Kind: KindString,
				Description: "Human-readable label for the constraint set"},
			{URN: "urn:x-nmos:cap:meta:preference", Name: "Constraint Set Preference", Kind: KindInteger,
				Description: "Preference ordering; higher is preferred", Min: f(-100), Max: f(100)},
			{URN: "urn:x-nmos:cap:meta:enabled", Name: "Constraint Set Enabled", Kind: KindBoolean,
				Description: "Whether the constraint set is currently active"},
		},
	})
}
