package rollcall

import (
	"context"
	"fmt"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// The control service is the same objects the menu describes, seen as values.
//
// Both generations are served from one store. Which structure a reply carries
// is decided by the message that asked, not by what the session negotiated: a
// client that sends the 16-bit request gets the 16-bit answer even on a session
// that could have used the other, because that is what it is waiting for.

// readValue answers a value read in whichever generation asked.
func (p *Provider) readValue(s *session.Session, prt *port, req codec.Frame) error {
	if prt == nil {
		return session.RefuseNack("no card in that slot")
	}
	if !s.Services().Has(codec.SvcControl) {
		return session.RefuseNack("this session did not ask for the control service")
	}

	if req.Type == codec.MsgGetValue {
		g, err := codec.DecodeGetValue(req.Payload)
		if err != nil {
			return session.RefuseNack("malformed value request")
		}
		v, ok := prt.value(g.Command)
		if !ok {
			return p.unknownCommand(prt, g.Command)
		}
		return answerValue(s, v)
	}

	g, err := codec.DecodeGetFStat(req.Payload)
	if err != nil {
		return session.RefuseNack("malformed status request")
	}
	v, ok := prt.value(uint32(g.Command))
	if !ok {
		return p.unknownCommand(prt, uint32(g.Command))
	}
	return answerFuncStatus(s, v)
}

// writeValue applies a write and answers with what was stored.
//
// The reply carries the stored value rather than an acknowledgement, which is
// what lets a client that asked for something out of range learn what it got
// without reading back.
func (p *Provider) writeValue(s *session.Session, prt *port, req codec.Frame) error {
	if prt == nil {
		return session.RefuseNack("no card in that slot")
	}
	if !s.Services().Has(codec.SvcControl) {
		return session.RefuseNack("this session did not ask for the control service")
	}

	if req.Type == codec.MsgSetValue {
		w, err := codec.DecodeValue(req.Payload)
		if err != nil {
			return session.RefuseNack("malformed value write")
		}
		stored, err := p.applyWrite(s, prt, w.Command, w.MatchID, w.Mode, w.Val, w.Text, w.Data)
		if err != nil {
			return err
		}
		return answerValue(s, stored)
	}

	w, err := codec.DecodeFuncStatus(req.Payload)
	if err != nil {
		return session.RefuseNack("malformed parameter write")
	}
	stored, err := p.applyWrite(s, prt, uint32(w.Command), w.MatchID, w.Mode, w.Value, w.Text, w.Data)
	if err != nil {
		return err
	}
	return answerFuncStatus(s, stored)
}

// applyWrite stores a value and tells every other subscriber.
//
// A write that names a unit type applies only if the type is ours or zero.
// That is how a controller writes one command to a whole frame without having
// to know which slots hold which cards: the cards it does not mean ignore it
// and answer with what they already had.
func (p *Provider) applyWrite(s *session.Session, prt *port, command uint32,
	matchID uint16, mode codec.Mode, num int32, text string, data []byte) (codec.Value, error) {

	if mode.Has(codec.ModeMatchID) && matchID != 0 && matchID != prt.id.TypeID {
		v, ok := prt.value(command)
		if !ok {
			return codec.Value{}, p.unknownCommand(prt, command)
		}
		return v, nil
	}

	// The gateway's own page carries one writable line, and writing it has to
	// do the thing rather than remember that somebody asked. A control that
	// stored a value and changed nothing would be worse than no control.
	if prt.number == 0 && command == cmdGatewayDebugLog {
		if err := p.setDebugLogging(num != 0); err != nil {
			return codec.Value{}, err
		}
	}

	stored, err := prt.setValue(command, mode, num, text, data)
	if err != nil {
		return codec.Value{}, session.RefuseNack(err.Error())
	}

	p.publishExcept(context.Background(), s, prt.number, stored)

	// A router's state is published on two nodes and a write to either has to
	// move both, or the panel and the tables disagree with nothing on the wire
	// to say which is right.
	p.syncRouterWrite(context.Background(), s, prt, stored)

	// What the node holds now is what the reply carries, which is the whole
	// point of answering with a value rather than an acknowledgement: a write
	// the device did something else with says so in the reply. Routing by
	// association is the case that needs it — the request names two
	// associations and the reply is the result of trying.
	if v, ok := prt.value(command); ok {
		stored = v
	}
	return stored, nil
}

// unknownCommand refuses a command this card does not have, and records it.
//
// A client asking for a command that is not in the menu it walked has either
// cached a menu that has since changed or invented a number; either way the
// refusal is the answer, and the count is what an operator needs to see that
// it is happening.
func (p *Provider) unknownCommand(prt *port, command uint32) error {
	p.fire(EventUnknownCommand, fmt.Sprintf(
		"slot %d was asked for command %d, which is not in its menu", prt.number, command))
	return session.RefuseNack(fmt.Sprintf("no command %d", command))
}

// answerValue replies with the 32-bit structure.
func answerValue(s *session.Session, v codec.Value) error {
	payload, err := v.AppendTo(nil)
	if err != nil {
		return err
	}
	return s.Answer(codec.MsgRetValue, payload)
}

// answerFuncStatus replies with the 16-bit structure.
//
// The command always fits: a 16-bit request could only have named a 16-bit
// number, and the value came back under that number. A wide command is
// unreachable from this generation by construction rather than by a check.
func answerFuncStatus(s *session.Session, v codec.Value) error {
	// The only way this encode fails is a string too long for the fixed field,
	// and funcStatusOf has already cut it to fit.
	payload, _ := funcStatusOf(v).AppendTo(nil)
	return s.Answer(codec.MsgRetFStat, payload)
}

// funcStatusOf renders a stored value in the older generation's structure,
// with the string cut to the fixed field it has there.
func funcStatusOf(v codec.Value) codec.FuncStatus {
	return codec.FuncStatus{
		Command: uint16(v.Command),
		Mode:    v.Mode,
		Value:   v.Val,
		Text:    codec.TruncateFixed(v.Text, codec.MaxTextSize),
		Data:    v.Data,
	}
}
