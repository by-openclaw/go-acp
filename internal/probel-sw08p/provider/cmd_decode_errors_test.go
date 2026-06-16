package probelsw08p

import (
	"errors"
	"testing"

	"dhs/internal/probel-sw08p/codec"
)

// Each per-command handler begins with a Decode call and returns the
// decode error unchanged. The framer pre-validates the §2 envelope, but
// a payload that is well-framed yet too short for the command body still
// reaches the handler, so the guard is live. We drive every guard with a
// frame carrying the correct CMD id and an empty (too-short) payload and
// assert the handler surfaces ErrShortPayload — the real reject path the
// session turns into a logged HandlerRejected + no reply.
func TestHandlerDecodeErrorGuards(t *testing.T) {
	srv := newComplianceServer(t)

	cases := []struct {
		name string
		id   codec.CommandID
	}{
		{"crosspoint_interrogate", codec.RxCrosspointInterrogate},
		{"crosspoint_connect", codec.RxCrosspointConnect},
		{"maintenance", codec.RxMaintenance},
		{"protect_interrogate", codec.RxProtectInterrogate},
		{"protect_connect", codec.RxProtectConnect},
		{"protect_disconnect", codec.RxProtectDisconnect},
		{"protect_device_name", codec.RxProtectDeviceNameRequest},
		{"protect_tally_dump", codec.RxProtectTallyDumpRequest},
		{"crosspoint_tally_dump", codec.RxCrosspointTallyDumpRequest},
		{"master_protect_connect", codec.RxMasterProtectConnect},
		{"all_source_names", codec.RxAllSourceNamesRequest},
		{"single_source_name", codec.RxSingleSourceNameRequest},
		{"all_dest_assoc_names", codec.RxAllDestNamesRequest},
		{"single_dest_assoc_name", codec.RxSingleDestNameRequest},
		{"tie_line_interrogate", codec.RxCrosspointTieLineInterrogate},
		{"all_source_assoc_names", codec.RxAllSourceAssocNamesRequest},
		{"single_source_assoc_name", codec.RxSingleSourceAssocNameRequest},
		{"update_name", codec.RxUpdateNameRequest},
		{"salvo_connect_on_go", codec.RxCrosspointConnectOnGoSalvo},
		{"salvo_go", codec.RxCrosspointGoSalvo},
		{"salvo_group_interrogate", codec.RxCrosspointSalvoGroupInterrogate},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := srv.handle(codec.Frame{ID: tc.id, Payload: nil})
			if err == nil {
				t.Fatalf("handle(%s, empty payload): want decode error, got nil", tc.name)
			}
			if !errors.Is(err, codec.ErrShortPayload) {
				t.Errorf("handle(%s): err = %v; want ErrShortPayload", tc.name, err)
			}
			if res.reply != nil || len(res.tallies) != 0 || res.streamToSender != nil {
				t.Errorf("handle(%s): want empty handlerResult on decode error; got %+v", tc.name, res)
			}
		})
	}
}

// handleDualControllerStatus's only decode-error path is a wrong CMD id
// (the request carries no payload, so ErrShortPayload is unreachable).
// Call the handler directly with a mismatched id to exercise the guard.
func TestDualControllerStatusWrongCommandGuard(t *testing.T) {
	srv := newComplianceServer(t)
	_, err := srv.handleDualControllerStatus(codec.Frame{ID: codec.RxCrosspointConnect})
	if err == nil {
		t.Fatal("handleDualControllerStatus(wrong id): want error, got nil")
	}
	if !errors.Is(err, codec.ErrWrongCommand) {
		t.Errorf("err = %v; want ErrWrongCommand", err)
	}
}
