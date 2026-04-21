package probel

import (
	"context"
	"fmt"

	iprobel "acp/internal/probel"
	"acp/internal/protocol"
)

// ProtectInterrogate asks the matrix "what is the protect state of this
// destination?". Replies with tx 011 Protect Tally carrying the owning
// device ID + one of the four ProtectState values.
//
// Reference: SW-P-88 §5.13 / §5.14. TS rx/010/ + tx/011/.
func (p *Plugin) ProtectInterrogate(
	ctx context.Context,
	matrix, level uint8,
	dst, device uint16,
) (iprobel.ProtectTallyParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.ProtectTallyParams{}, err
	}
	req := iprobel.EncodeProtectInterrogate(iprobel.ProtectInterrogateParams{
		MatrixID: matrix, LevelID: level, DestinationID: dst, DeviceID: device,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxProtectTally || f.ID == iprobel.TxProtectTallyExt
	})
	if err != nil {
		return iprobel.ProtectTallyParams{}, fmt.Errorf("probel protect-interrogate: %w", err)
	}
	t, derr := iprobel.DecodeProtectTally(reply)
	if derr != nil {
		return iprobel.ProtectTallyParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return t, nil
}

// ProtectConnect requests protection on (matrix, level, dst) owned by
// device. Reply is tx 013 Protect Connected (same wire shape as tally).
// Other sessions also see a tx 011 Protect Tally broadcast.
//
// Reference: SW-P-88 §5.15. TS rx/012/.
func (p *Plugin) ProtectConnect(
	ctx context.Context,
	matrix, level uint8,
	dst, device uint16,
) (iprobel.ProtectConnectedParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.ProtectConnectedParams{}, err
	}
	req := iprobel.EncodeProtectConnect(iprobel.ProtectConnectParams{
		MatrixID: matrix, LevelID: level, DestinationID: dst, DeviceID: device,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxProtectConnected || f.ID == iprobel.TxProtectConnectedExt
	})
	if err != nil {
		return iprobel.ProtectConnectedParams{}, fmt.Errorf("probel protect-connect: %w", err)
	}
	c, derr := iprobel.DecodeProtectConnected(reply)
	if derr != nil {
		return iprobel.ProtectConnectedParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return c, nil
}

// ProtectDisconnect releases the protect on (matrix, level, dst) owned
// by device. Reply is tx 015 Protect Disconnected; tally broadcast to
// other sessions as tx 011 with state=ProtectNone.
//
// Reference: SW-P-88 §5.17. TS rx/014/.
func (p *Plugin) ProtectDisconnect(
	ctx context.Context,
	matrix, level uint8,
	dst, device uint16,
) (iprobel.ProtectDisconnectedParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.ProtectDisconnectedParams{}, err
	}
	req := iprobel.EncodeProtectDisconnect(iprobel.ProtectDisconnectParams{
		MatrixID: matrix, LevelID: level, DestinationID: dst, DeviceID: device,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxProtectDisconnected || f.ID == iprobel.TxProtectDisconnectedExt
	})
	if err != nil {
		return iprobel.ProtectDisconnectedParams{}, fmt.Errorf("probel protect-disconnect: %w", err)
	}
	d, derr := iprobel.DecodeProtectDisconnected(reply)
	if derr != nil {
		return iprobel.ProtectDisconnectedParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return d, nil
}

// ProtectDeviceName resolves a deviceID (0-1023) to its 8-char ASCII
// name as held in the matrix's device table. Wire form is the left-
// space-padded 8-byte name trimmed by the decoder.
//
// Reference: SW-P-88 §5.21. TS rx/017/ + tx/018/.
func (p *Plugin) ProtectDeviceName(
	ctx context.Context,
	device uint16,
) (string, error) {
	cli, err := p.getClient()
	if err != nil {
		return "", err
	}
	req := iprobel.EncodeProtectDeviceNameRequest(iprobel.ProtectDeviceNameRequestParams{DeviceID: device})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxProtectDeviceNameResponse
	})
	if err != nil {
		return "", fmt.Errorf("probel protect-name: %w", err)
	}
	r, derr := iprobel.DecodeProtectDeviceNameResponse(reply)
	if derr != nil {
		return "", &protocol.TransportError{Op: "decode", Err: derr}
	}
	return r.DeviceName, nil
}

// ProtectTallyDump requests the protect table for one (matrix, level),
// starting at firstDst. Reply is tx 020 Protect Tally Dump — one frame
// for our demo matrix. Large protect tables may be split across frames
// on the wire; this consumer returns the first frame only (caller
// iterates with a fresh firstDst for multi-frame dumps).
//
// Reference: SW-P-88 §5.19 / §5.20. TS rx/019/ + tx/020/.
func (p *Plugin) ProtectTallyDump(
	ctx context.Context,
	matrix, level uint8,
	firstDst uint16,
) (iprobel.ProtectTallyDumpParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.ProtectTallyDumpParams{}, err
	}
	req := iprobel.EncodeProtectTallyDumpRequest(iprobel.ProtectTallyDumpRequestParams{
		MatrixID: matrix, LevelID: level, DestinationID: firstDst,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxProtectTallyDump || f.ID == iprobel.TxProtectTallyDumpExt
	})
	if err != nil {
		return iprobel.ProtectTallyDumpParams{}, fmt.Errorf("probel protect-dump: %w", err)
	}
	d, derr := iprobel.DecodeProtectTallyDump(reply)
	if derr != nil {
		return iprobel.ProtectTallyDumpParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return d, nil
}

// MasterProtectConnect is the override-flavoured Protect Connect used
// by master panels to seize a protect already held by another panel.
// Reply is a normal tx 013 Protect Connected broadcast.
//
// Reference: SW-P-88 §5.31. TS rx/029/.
func (p *Plugin) MasterProtectConnect(
	ctx context.Context,
	matrix, level uint8,
	dst, device uint16,
) (iprobel.ProtectConnectedParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.ProtectConnectedParams{}, err
	}
	req := iprobel.EncodeMasterProtectConnect(iprobel.MasterProtectConnectParams{
		MatrixID: matrix, LevelID: level, DestinationID: dst, DeviceID: device,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxProtectConnected || f.ID == iprobel.TxProtectConnectedExt
	})
	if err != nil {
		return iprobel.ProtectConnectedParams{}, fmt.Errorf("probel master-protect: %w", err)
	}
	c, derr := iprobel.DecodeProtectConnected(reply)
	if derr != nil {
		return iprobel.ProtectConnectedParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return c, nil
}
