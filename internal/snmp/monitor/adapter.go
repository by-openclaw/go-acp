// Package snmpmon adapts an SNMP session to the neutral consumer.Protocol
// interface so the ADR-0030 device monitor can poll SNMP agents like any
// other connector.
//
// SNMP has no "any OID changed" notification (only vendor-defined traps,
// only on transition), so an SNMP device is monitored by polling. This
// adapter is the bridge: the monitor calls GetValue/SetValue on the
// neutral interface; the adapter turns each into an SNMP GET/SET and maps
// the SMI value syntaxes to neutral consumer.Value kinds.
package snmpmon

import (
	"context"
	"fmt"
	"net"

	dhsc "dhs/internal/consumer"
	"dhs/internal/snmp/codec"
)

// Session is the slice of an SNMP session the adapter needs. *consumer.Session
// satisfies it; tests supply a fake.
type Session interface {
	Get(ctx context.Context, names ...codec.OID) ([]codec.VarBind, error)
	Set(ctx context.Context, binds ...codec.VarBind) ([]codec.VarBind, error)
	Close() error
}

// Protocol implements consumer.Protocol over an SNMP session. Only
// GetValue and SetValue reach the wire; the monitor uses no others.
type Protocol struct {
	sess Session
}

// New wraps an SNMP session as a neutral Protocol.
func New(sess Session) *Protocol { return &Protocol{sess: sess} }

// Connect is a no-op: the session is dialed by the caller and handed in
// already connected.
func (p *Protocol) Connect(ctx context.Context, ip string, port int) error { return nil }

// Disconnect closes the underlying session.
func (p *Protocol) Disconnect() error {
	if p.sess == nil {
		return nil
	}
	return p.sess.Close()
}

// GetValue reads one object with an SNMP GET.
func (p *Protocol) GetValue(ctx context.Context, req dhsc.ValueRequest) (dhsc.Value, error) {
	oid, err := requestOID(req)
	if err != nil {
		return dhsc.Value{}, err
	}
	binds, err := p.sess.Get(ctx, oid)
	if err != nil {
		return dhsc.Value{}, err
	}
	if len(binds) == 0 {
		return dhsc.Value{}, fmt.Errorf("snmp monitor: empty response for %s", oid)
	}
	v := binds[0].Value
	if v.IsException() {
		return dhsc.Value{}, fmt.Errorf("snmp monitor: %s returned %s", oid, v.Type)
	}
	return toNeutral(v), nil
}

// SetValue writes one object with an SNMP SET and returns the echoed value.
func (p *Protocol) SetValue(ctx context.Context, req dhsc.ValueRequest, val dhsc.Value) (dhsc.Value, error) {
	oid, err := requestOID(req)
	if err != nil {
		return dhsc.Value{}, err
	}
	cv, err := fromNeutral(val)
	if err != nil {
		return dhsc.Value{}, err
	}
	binds, err := p.sess.Set(ctx, codec.VarBind{Name: oid, Value: cv})
	if err != nil {
		return dhsc.Value{}, err
	}
	if len(binds) == 0 {
		return val, nil
	}
	return toNeutral(binds[0].Value), nil
}

// The remaining neutral methods are not used by the monitor; they return
// a clear error rather than pretend.
func (p *Protocol) GetDeviceInfo(ctx context.Context) (dhsc.DeviceInfo, error) {
	return dhsc.DeviceInfo{}, unsupported("GetDeviceInfo")
}

func (p *Protocol) GetSlotInfo(ctx context.Context, slot int) (dhsc.SlotInfo, error) {
	return dhsc.SlotInfo{}, unsupported("GetSlotInfo")
}

func (p *Protocol) Walk(ctx context.Context, slot int) ([]dhsc.Object, error) {
	return nil, unsupported("Walk")
}

func (p *Protocol) Subscribe(req dhsc.ValueRequest, fn dhsc.EventFunc) error {
	return unsupported("Subscribe")
}

func (p *Protocol) Unsubscribe(req dhsc.ValueRequest) error {
	return unsupported("Unsubscribe")
}

func unsupported(method string) error {
	return fmt.Errorf("snmp monitor adapter: %s is not supported (the monitor uses GetValue/SetValue)", method)
}

// requestOID resolves a neutral request to a dotted SNMP OID. The monitor
// carries the OID in Path (an SNMP profile's "oid" feeds Path); Label is
// accepted too.
func requestOID(req dhsc.ValueRequest) (codec.OID, error) {
	s := req.Path
	if s == "" {
		s = req.Label
	}
	if s == "" {
		return nil, fmt.Errorf("snmp monitor: request has no OID (set path or label)")
	}
	return codec.ParseOID(s)
}

// toNeutral maps an SMI value syntax to a neutral kind.
func toNeutral(v codec.Value) dhsc.Value {
	switch v.Type {
	case codec.TypeInteger:
		return dhsc.Value{Kind: dhsc.KindInt, Int: v.Int}
	case codec.TypeCounter32, codec.TypeGauge32, codec.TypeTimeTicks, codec.TypeCounter64:
		return dhsc.Value{Kind: dhsc.KindUint, Uint: v.Uint}
	case codec.TypeOctetString, codec.TypeOpaque:
		return dhsc.Value{Kind: dhsc.KindString, Str: string(v.Bytes)}
	case codec.TypeOID:
		return dhsc.Value{Kind: dhsc.KindString, Str: v.OID.String()}
	case codec.TypeIPAddress:
		var a [4]byte
		if ip4 := v.IP.To4(); ip4 != nil {
			copy(a[:], ip4)
		}
		return dhsc.Value{Kind: dhsc.KindIPAddr, IPAddr: a}
	default:
		return dhsc.Value{Kind: dhsc.KindUnknown}
	}
}

// fromNeutral maps a neutral value back to an SMI syntax for a SET. The
// mapping is best-effort: neutral kinds are broader than SNMP types, and
// KindUint becomes Gauge32. A caller needing a specific integer syntax
// (e.g. a device that only accepts Integer32) should write through the
// SNMP consumer directly.
func fromNeutral(v dhsc.Value) (codec.Value, error) {
	switch v.Kind {
	case dhsc.KindInt:
		return codec.Int(v.Int), nil
	case dhsc.KindEnum:
		return codec.Int(int64(v.Enum)), nil
	case dhsc.KindUint:
		return codec.Gauge32(uint32(v.Uint)), nil
	case dhsc.KindString:
		return codec.String(v.Str), nil
	case dhsc.KindIPAddr:
		return codec.IPAddress(net.IP(v.IPAddr[:])), nil
	case dhsc.KindBool:
		if v.Bool {
			return codec.Int(1), nil
		}
		return codec.Int(0), nil
	default:
		return codec.Value{}, fmt.Errorf("snmp monitor: cannot write value kind %d over SNMP", v.Kind)
	}
}
