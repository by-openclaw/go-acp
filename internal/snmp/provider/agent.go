package provider

import (
	"log/slog"

	"dhs/internal/plugin"
	"dhs/internal/snmp/codec"
)

// Communities are the v1/v2c passwords. They are passwords in the sense
// that they travel in clear text on every datagram, which is why v3
// exists — but they are still the whole of the access control an agent
// has under v1 and v2c, and getting them wrong means either a manager
// that cannot poll or a plant anybody can write to.
type Communities struct {
	// Read admits GET, GETNEXT and GETBULK. Empty means "public",
	// because that is what every manager tries first and an agent
	// nobody can read is not an agent.
	Read string
	// Write admits SET. Empty means SET is refused for every community,
	// including the read one — a plant where the read password is also
	// the write password is a plant one typo away from a re-route.
	Write string
}

// DefaultReadCommunity is what an empty Communities.Read means.
const DefaultReadCommunity = "public"

// read resolves the default once, so the check below reads as the rule
// rather than as the defaulting. There is no matching write(): an empty
// write community is not a default, it is a refusal.
func (c Communities) read() string {
	if c.Read == "" {
		return DefaultReadCommunity
	}
	return c.Read
}

// Agent answers SNMP requests against a [MIB].
//
// It has no socket, no goroutine and no state beyond the tree and the
// communities: Respond takes one message and returns one message. That
// is what makes the rules testable — RFC 1157 and RFC 3416 differ on
// almost every error case, and those differences are what a manager
// actually sees.
type Agent struct {
	mib         *MIB
	communities Communities
	logger      *slog.Logger

	// maxSize bounds the response. A response that will not fit is
	// answered with tooBig and empty varbinds, per RFC 3416 §4.2.1 —
	// NOT truncated, because a manager cannot tell a truncated table
	// from the end of one.
	maxSize int
}

// NewAgent builds an agent over a tree.
func NewAgent(mib *MIB, communities Communities, logger *slog.Logger) *Agent {
	return &Agent{
		mib:         mib,
		communities: communities,
		logger:      plugin.LoggerOrDefault(logger),
		maxSize:     codec.MaxMessageSize,
	}
}

// Respond answers one request, returning both the response and the
// datagram that carries it.
//
// The bytes come back because the size rule cannot be applied without
// them: RFC 3416 §4.2.1 asks for tooBig when a response does not FIT,
// which is a fact about the encoding. Returning what was already encoded
// spares the caller a second pass over every varbind on the reply path.
//
// The bool is whether to send anything at all. An agent is SILENT rather
// than informative when it will not serve a request: a wrong community
// or a PDU an agent has no business receiving gets no datagram back, so
// that a scanner learns nothing from the difference between a wrong
// password and a closed port. RFC 1157 §4.1 says as much, and it is also
// why a community mismatch is worth a log line here — it is the only
// place it will ever be visible.
func (a *Agent) Respond(req codec.Message) (codec.Message, []byte, bool) {
	if req.PDU == nil {
		// A v1 trap, or a message with nothing in it. Either way an
		// agent does not answer it.
		return codec.Message{}, nil, false
	}
	p := req.PDU

	switch p.Type {
	case codec.PDUTypeGet, codec.PDUTypeGetNext, codec.PDUTypeGetBulk, codec.PDUTypeSet:
	default:
		// A Response or a notification arriving at an agent is somebody
		// else's traffic, or a reflection attempt. Dropped.
		a.logger.Debug("snmp agent: ignoring a PDU an agent does not answer",
			"type", p.Type.String())
		return codec.Message{}, nil, false
	}

	if !a.admits(req.Community, p.Type) {
		a.logger.Warn("snmp agent: refused community",
			"type", p.Type.String(), "version", req.Version.String())
		return codec.Message{}, nil, false
	}

	out := codec.PDU{
		Type:      codec.PDUTypeResponse,
		RequestID: p.RequestID,
	}

	switch p.Type {
	case codec.PDUTypeGet:
		out.VarBinds, out.ErrorStatus, out.ErrorIndex = a.get(req.Version, p.VarBinds)
	case codec.PDUTypeGetNext:
		out.VarBinds, out.ErrorStatus, out.ErrorIndex = a.getNext(req.Version, p.VarBinds)
	case codec.PDUTypeGetBulk:
		out.VarBinds = a.getBulk(p)
	case codec.PDUTypeSet:
		out.VarBinds, out.ErrorStatus, out.ErrorIndex = a.set(req.Version, p.VarBinds)
	}

	resp := codec.Message{Version: req.Version, Community: req.Community, PDU: &out}

	raw, err := codec.Encode(resp)
	if err == nil && len(raw) <= a.maxSize {
		return resp, raw, true
	}

	// RFC 3416 §4.2.1: a response that does not fit is REPLACED by
	// tooBig carrying no bindings, never truncated — a manager cannot
	// tell a short table from the end of a table.
	a.logger.Debug("snmp agent: response replaced by tooBig",
		"request", p.RequestID, "bytes", len(raw))
	out.ErrorStatus, out.ErrorIndex = codec.TooBig, 0
	out.VarBinds = nil

	raw, err = codec.Encode(resp)
	if err != nil {
		// Not even the empty response encodes, which means the request
		// named a version this package cannot answer in. Nothing useful
		// can be sent, so nothing is.
		a.logger.Error("snmp agent: no response can be encoded",
			"request", p.RequestID, "version", req.Version.String(), "err", err)
		return codec.Message{}, nil, false
	}
	return resp, raw, true
}

// admits checks the community for the operation.
func (a *Agent) admits(community string, t codec.PDUType) bool {
	if t == codec.PDUTypeSet {
		// An empty write community means writes are refused outright,
		// not that any community may write.
		return a.communities.Write != "" && community == a.communities.Write
	}
	return community == a.communities.read()
}

// get answers a GET: every name must resolve, and v1 and v2c disagree
// about what happens when one does not.
func (a *Agent) get(v codec.Version, in []codec.VarBind) ([]codec.VarBind, codec.ErrorStatus, int) {
	out := make([]codec.VarBind, 0, len(in))
	for i, vb := range in {
		obj, ok := a.mib.Get(vb.Name)
		if !ok || obj.Access == NotAccessible {
			if v == codec.Version1 {
				// v1 has no exception values: the whole request fails,
				// and error-index is 1-based so the manager can say
				// WHICH name it got wrong.
				return echo(in), codec.NoSuchName, i + 1
			}
			out = append(out, codec.VarBind{Name: vb.Name, Value: a.missing(vb.Name)})
			continue
		}
		out = append(out, codec.VarBind{Name: obj.OID, Value: obj.Get()})
	}
	return out, codec.NoError, 0
}

// missing distinguishes "there is no such object here" from "the object
// is real but has no such instance", which is the difference between a
// manager giving up on a branch and retrying with a different index.
//
// This tree holds INSTANCES, not the object types an SMI module
// declares, so the distinction is inferred the way net-snmp infers it —
// from whether anything registered lives at or under the name once its
// instance sub-identifier is dropped:
//
//   - something under this exact name: the name is an object type asked
//     for without an instance (a GET of sysDescr rather than
//     sysDescr.0). noSuchInstance.
//   - something under the name with its last arc removed: the object
//     type is real and THIS instance is not (ifDescr.99 on a two-port
//     box). noSuchInstance.
//   - neither: nothing here at all. noSuchObject.
func (a *Agent) missing(name codec.OID) codec.Value {
	if a.hasSubtree(name) {
		return codec.NoSuchInstance()
	}
	if len(name) > 1 && a.hasSubtree(name[:len(name)-1]) {
		return codec.NoSuchInstance()
	}
	return codec.NoSuchObject()
}

// hasSubtree reports whether the tree holds anything at or under prefix.
func (a *Agent) hasSubtree(prefix codec.OID) bool {
	next, ok := a.mib.Next(prefix)
	return ok && next.OID.HasPrefix(prefix)
}

// getNext answers a GETNEXT: each name is replaced by the next object
// after it, which is how a manager walks a tree it has no MIB for.
func (a *Agent) getNext(v codec.Version, in []codec.VarBind) ([]codec.VarBind, codec.ErrorStatus, int) {
	out := make([]codec.VarBind, 0, len(in))
	for i, vb := range in {
		obj, ok := a.mib.Next(vb.Name)
		if !ok {
			if v == codec.Version1 {
				// v1's only way to say "the walk is over".
				return echo(in), codec.NoSuchName, i + 1
			}
			out = append(out, codec.VarBind{Name: vb.Name, Value: codec.EndOfMIBView()})
			continue
		}
		out = append(out, codec.VarBind{Name: obj.OID, Value: obj.Get()})
	}
	return out, codec.NoError, 0
}

// getBulk answers a GETBULK: the first non-repeaters names get one step
// each, and every name after them gets max-repetitions steps.
//
// v2c only, and it never reports an error — a bulk that walks off the
// end of the tree fills the remaining slots with endOfMibView, which is
// what tells the manager to stop asking.
func (a *Agent) getBulk(p *codec.PDU) []codec.VarBind {
	nonRepeaters := p.NonRepeaters
	if nonRepeaters < 0 {
		// A negative count is a malformed request rather than a
		// negative number of names; RFC 3416 §4.2.3 says to treat it
		// as zero rather than to refuse, so a manager's off-by-one
		// still gets an answer it can use.
		nonRepeaters = 0
	}
	if nonRepeaters > len(p.VarBinds) {
		nonRepeaters = len(p.VarBinds)
	}
	maxReps := p.MaxRepetitions
	if maxReps < 0 {
		maxReps = 0
	}

	var out []codec.VarBind
	for _, vb := range p.VarBinds[:nonRepeaters] {
		out = append(out, a.step(vb.Name))
	}

	repeaters := p.VarBinds[nonRepeaters:]
	// Walk in rounds rather than per name, so the varbinds come back in
	// the order RFC 3416 §4.2.3 lays down: all the first repetitions,
	// then all the second. A manager reassembling a table by column
	// depends on it.
	cursors := make([]codec.OID, len(repeaters))
	for i, vb := range repeaters {
		cursors[i] = vb.Name
	}
	for r := 0; r < maxReps; r++ {
		for i := range cursors {
			vb := a.step(cursors[i])
			out = append(out, vb)
			cursors[i] = vb.Name
		}
	}
	return out
}

// step is one GETNEXT hop, used by GETBULK for each repetition.
func (a *Agent) step(from codec.OID) codec.VarBind {
	obj, ok := a.mib.Next(from)
	if !ok {
		return codec.VarBind{Name: from, Value: codec.EndOfMIBView()}
	}
	return codec.VarBind{Name: obj.OID, Value: obj.Get()}
}

// set answers a SET.
//
// RFC 3416 §4.2.5 wants a set to be atomic: either every varbind is
// applied or none is. So every write is CHECKED first and only then
// applied — a two-phase pass, rather than applying until one fails and
// leaving the device half-configured. What this cannot undo is a Set
// implementation that fails after its predecessor succeeded; that is
// reported as commitFailed, which is precisely what the status means.
func (a *Agent) set(v codec.Version, in []codec.VarBind) ([]codec.VarBind, codec.ErrorStatus, int) {
	for i, vb := range in {
		obj, ok := a.mib.Get(vb.Name)
		switch {
		case !ok || obj.Access == NotAccessible:
			if v == codec.Version1 {
				return echo(in), codec.NoSuchName, i + 1
			}
			return echo(in), codec.NoCreation, i + 1
		case obj.Access != ReadWrite:
			if v == codec.Version1 {
				// v1 has no notWritable; readOnly is the whole of what
				// it can say.
				return echo(in), codec.ReadOnly, i + 1
			}
			return echo(in), codec.NotWritable, i + 1
		case vb.Value.Type != obj.Type:
			if v == codec.Version1 {
				return echo(in), codec.BadValue, i + 1
			}
			return echo(in), codec.WrongType, i + 1
		}
	}

	for i, vb := range in {
		obj, _ := a.mib.Get(vb.Name)
		if err := obj.Set(vb.Value); err != nil {
			a.logger.Warn("snmp agent: set refused by the object",
				"oid", vb.Name.String(), "err", err)
			if v == codec.Version1 {
				return echo(in), codec.BadValue, i + 1
			}
			if i == 0 {
				// Nothing was applied, so the device is where it was.
				return echo(in), codec.WrongValue, i + 1
			}
			// Earlier writes landed. commitFailed is the status that
			// says exactly that, and hiding it as wrongValue would tell
			// the manager the device is untouched when it is not.
			return echo(in), codec.CommitFailed, i + 1
		}
	}

	// A successful set echoes the values as written, which is how a
	// manager confirms what the device actually took.
	return echo(in), codec.NoError, 0
}

// echo returns the request's varbinds, which is what every error
// response carries: RFC 3416 §4.2.1 says a response on error repeats the
// request's bindings so the manager can line error-index up against the
// names it sent.
func echo(in []codec.VarBind) []codec.VarBind {
	out := make([]codec.VarBind, len(in))
	copy(out, in)
	return out
}
