package codec

import "testing"

// The two generations do the same jobs with different message numbers, so a
// message says which one it belongs to and most say neither.
func TestPacketTypeGeneration(t *testing.T) {
	for _, tc := range []struct {
		typ  PacketType
		want Generation
	}{
		{MsgGetFunc, Gen16},
		{MsgRetFunc, Gen16},
		{MsgGetFStat, Gen16},
		{MsgRetFStat, Gen16},
		{MsgSetParam, Gen16},
		{MsgGetMenuCount, Gen32},
		{MsgRetMenuCount, Gen32},
		{MsgGetMenuItem, Gen32},
		{MsgRetMenuItem, Gen32},
		{MsgGetValue, Gen32},
		{MsgRetValue, Gen32},
		{MsgSetValue, Gen32},
		// The rest are the same in both, which is most of the protocol.
		{MsgGetID, GenAny},
		{MsgGetStat, GenAny},
		{MsgKeepAlive, GenAny},
		{MsgIam, GenAny},
		{MsgGetLocDevMap, GenAny},
		{MsgFileOpen, GenAny},
	} {
		if got := tc.typ.Generation(); got != tc.want {
			t.Errorf("%s is %s, want %s", tc.typ, got, tc.want)
		}
	}
}

func TestGenerationNames(t *testing.T) {
	for _, tc := range []struct {
		g    Generation
		want string
	}{
		{Gen16, "16-bit"},
		{Gen32, "32-bit"},
		{GenAny, "either"},
		{Generation(99), "either"},
	} {
		if got := tc.g.String(); got != tc.want {
			t.Errorf("Generation(%d) = %q, want %q", tc.g, got, tc.want)
		}
	}
}
