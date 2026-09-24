package consumer

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	dhsc "dhs/internal/consumer"
	"dhs/internal/consumer/monitor"
	"dhs/internal/consumer/pollwatch"
	"dhs/internal/plugin"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mib"
)

// The manager above is SNMP's own shape — get, walk, set, traps. This
// file is the other face: the same session behind the neutral
// consumer.Protocol, so an agent answers `tree`, `export`, `watch` and
// `alarm` like every other device in this repo. Nothing SNMP-specific
// leaks out of it; what the compiled MIB knows (names, access, enum
// items) becomes what the DM carries, which is why a walked agent can
// be judged by the same alarm engine as an ACP2 shelf.

// Name is the registry name: dhs consumer snmp <verb>.
const Name = "snmp"

// staleAfter is how long a polled value is treated as current.
const staleAfter = 30 * time.Second

// walkLimit caps one Walk: a device whose table grows while it is
// being walked would otherwise never end. It is deliberately far above
// what any device here publishes — a DR5000's 4096-row programme
// stream table alone is 20 000 objects, and a cap that truncates the
// model silently is worse than a slow walk. Truncation is logged.
var walkLimit = 200000

func init() {
	dhsc.Register(&Factory{})
}

// Factory builds Plugin instances.
type Factory struct{}

// Meta describes the plugin to the registry.
func (f *Factory) Meta() dhsc.ProtocolMeta {
	return dhsc.ProtocolMeta{
		Name:        Name,
		DefaultPort: DefaultPort,
		Description: "SNMP v1 / v2c manager — the device model is the agent's own MIB tree",
	}
}

// New builds a plugin from the injected dependency set.
func (f *Factory) New(deps plugin.Deps) dhsc.Protocol {
	deps = deps.WithDefaults()
	p := &Plugin{deps: deps, interval: 10 * time.Second}
	p.Init(deps, staleAfter)
	p.poller = pollwatch.New(p, p.pollProfileFor, deps.Logger, deps.Clock)
	return p
}

// Plugin is one agent. An agent has no slots, so everything lives on
// slot 0 — the shape every other single-box connector already uses.
type Plugin struct {
	dhsc.Base

	deps plugin.Deps

	mu       sync.Mutex
	sess     *Session
	host     string
	sysOID   codec.OID
	tree      []dhsc.Object
	byPath    map[string]codec.OID
	paths     map[string]codec.OID
	community string
	writeCmty string
	version   codec.Version
	writeSess *Session
	port      int
	interval time.Duration

	poller *pollwatch.Poller
}

// Connect dials the agent. Version is not asked for: v2c is tried
// first and v1 second, because an agent that speaks only v1 answers a
// v2c request with silence — which is indistinguishable from a device
// that is down, and is the single most common way an IRD looks broken.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	if port <= 0 {
		port = DefaultPort
	}
	addr := fmt.Sprintf("%s:%d", ip, port)

	var firstErr error
	for _, v := range []codec.Version{codec.Version2c, codec.Version1} {
		// Retries is explicit: zero means "one attempt" in Options, and
		// UDP loses datagrams. A manager that does not retry reports a
		// device as down because one packet was dropped — which is
		// exactly what the first live run of the integration suite
		// caught, on a fabric where the IRD answers in ~700 ms.
		sess, err := Dial(ctx, Options{
			Addr: addr, Version: v, Community: p.community,
			Retries: DefaultRetries, Timeout: DefaultTimeout,
			// The connector's own profile: an agent's deviations are
			// counted where every other protocol's are, and `status`
			// can show them.
			Compliance: p.ComplianceProfile(),
		}, p.deps)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		// One real read decides it: a version the agent ignores times
		// out here, not halfway through a walk. It is also the read
		// that says which branch is the device's own, so every later
		// name resolves without a walk.
		binds, err := sess.Get(ctx, sysObjectID)
		if err != nil {
			_ = sess.Close()
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		p.mu.Lock()
		p.sess, p.host, p.port = sess, ip, port
		p.version = v
		if len(binds) > 0 && binds[0].Value.Type == codec.TypeOID {
			p.sysOID = binds[0].Value.OID
			p.paths = nil
		}
		p.mu.Unlock()
		p.Opened("udp", ip, port, dhsc.MetricsTimes{C: p.Metrics()})
		return nil
	}
	return fmt.Errorf("snmp: %s answered neither v2c nor v1: %w", addr, firstErr)
}

// Disconnect closes the session.
func (p *Plugin) Disconnect() error {
	p.mu.Lock()
	sess, wsess := p.sess, p.writeSess
	p.sess, p.writeSess = nil, nil
	p.mu.Unlock()
	if wsess != nil {
		_ = wsess.Close()
	}
	if sess == nil {
		return nil
	}
	p.Closed()
	return sess.Close()
}

func (p *Plugin) session() (*Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sess == nil {
		return nil, dhsc.ErrNotConnected
	}
	return p.sess, nil
}

var (
	sysDescr    = codec.OID{1, 3, 6, 1, 2, 1, 1, 1, 0}
	sysObjectID = codec.OID{1, 3, 6, 1, 2, 1, 1, 2, 0}
	sysName     = codec.OID{1, 3, 6, 1, 2, 1, 1, 5, 0}
	systemGroup = codec.OID{1, 3, 6, 1, 2, 1, 1}
)

// GetDeviceInfo answers from the system group. An agent is one box:
// one slot, always present.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (dhsc.DeviceInfo, error) {
	sess, err := p.session()
	if err != nil {
		return dhsc.DeviceInfo{}, err
	}
	binds, err := sess.Get(ctx, sysDescr, sysObjectID, sysName)
	if err != nil {
		return dhsc.DeviceInfo{}, fmt.Errorf("snmp: system group: %w", err)
	}
	p.RecordRx()
	p.mu.Lock()
	// Connect normalised the port before it stored it, so there is
	// nothing to default here.
	port, version := p.port, p.version
	p.mu.Unlock()
	info := dhsc.DeviceInfo{
		IP: p.host, Port: port, NumSlots: 1,
		// The version this session NEGOTIATED, not a constant. Reporting
		// v1 for a v2c session is how a walk that takes an hour looks
		// like the protocol's fault rather than a question worth asking
		// — the DR5000 answers both, and only one of them has GETBULK.
		// The neutral field is a number, so v2c reports 2.
		ProtocolVersion: protocolVersionNumber(version),
	}
	for _, b := range binds {
		if b.Name.Compare(sysObjectID) == 0 && b.Value.Type == codec.TypeOID {
			{
				oid := b.Value.OID
				p.mu.Lock()
				p.sysOID = oid
				p.mu.Unlock()
				info.DtdVersion = oid.String()
			}
		}
	}
	return info, nil
}

// protocolVersionNumber maps the wire version onto the neutral
// DeviceInfo's number. SNMP's versions are named 1, 2c and 3 while the
// wire encodes them 0, 1 and 3, and the neutral field is a number an
// operator reads — so v2c is reported as 2, which is what the manuals
// and the vendors call it.
func protocolVersionNumber(v codec.Version) int {
	switch v {
	case codec.Version2c:
		return 2
	case codec.Version3:
		return 3
	default:
		return 1
	}
}

// GetSlotInfo answers for the one slot an agent has.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (dhsc.SlotInfo, error) {
	if _, err := p.session(); err != nil {
		return dhsc.SlotInfo{}, err
	}
	if slot > 0 {
		return dhsc.SlotInfo{Slot: slot, Status: dhsc.SlotNoCard, State: dhsc.SlotStateNoCard}, nil
	}
	return dhsc.SlotInfo{
		Slot: 0, Status: dhsc.SlotPresent, State: dhsc.SlotStatePresent,
		IsOnline: true, LiveAt: p.Clock().Now(),
	}, nil
}

// Walk reads the device model: the system group, and the agent's own
// enterprise subtree — the branch its sysObjectID names, which is
// where a vendor puts everything that is actually about the device.
// The compiled MIB supplies the names, the access and the enum items,
// so the objects that come back carry what `alarm suggest` needs to
// write rules without anyone typing an OID.
func (p *Plugin) Walk(ctx context.Context, slot int) ([]dhsc.Object, error) {
	sess, err := p.session()
	if err != nil {
		return nil, err
	}
	if slot > 0 {
		return nil, fmt.Errorf("snmp: an agent has one slot (0), not %d", slot)
	}
	roots := []codec.OID{systemGroup}
	if ent := p.enterpriseRoot(ctx, sess); ent != nil {
		roots = append(roots, ent)
	}

	var objs []dhsc.Object
	errStop := fmt.Errorf("snmp: walk limit of %d objects", walkLimit)
	for _, root := range roots {
		// A walk that stopped early still returns what it read: a
		// device model that is 20 000 objects deep is still a model,
		// and an IRD's programme tables are that deep.
		werr := sess.Walk(ctx, root, func(b codec.VarBind) error {
			if len(objs) >= walkLimit {
				return errStop
			}
			objs = append(objs, p.objectFor(b))
			return nil
		})
		p.RecordRx()
		warn, fatal := walkOutcome(len(objs), werr, errStop)
		if fatal != nil {
			return nil, fatal
		}
		if warn {
			p.deps.Logger.Warn("snmp: the model is not complete",
				"root", root.String(), "objects", len(objs), "err", werr, "limit", walkLimit)
		}
	}

	p.mu.Lock()
	p.tree = objs
	p.byPath = make(map[string]codec.OID, len(objs))
	for _, o := range objs {
		// Parsed rather than Must: this is a device's answer, and a
		// connector does not panic on one.
		if oid, err := codec.ParseOID(o.OID); err == nil {
			p.byPath[strings.Join(o.Path, ".")] = oid
		}
	}
	p.mu.Unlock()
	return objs, nil
}

// enterpriseRoot is 1.3.6.1.4.1.<pen> for whatever sysObjectID says.
// An agent that answers no sysObjectID gets the system group alone.
func (p *Plugin) enterpriseRoot(ctx context.Context, sess *Session) codec.OID {
	p.mu.Lock()
	oid := p.sysOID
	p.mu.Unlock()
	if len(oid) == 0 {
		binds, err := sess.Get(ctx, sysObjectID)
		if err != nil {
			return nil
		}
		if oid = oidFromBinds(binds); oid == nil {
			return nil
		}
		p.mu.Lock()
		p.sysOID = oid
		p.mu.Unlock()
	}
	// 1.3.6.1.4.1.<pen>… — keep the enterprise number, drop the rest.
	if len(oid) < 8 || !oid.HasPrefix(codec.OID{1, 3, 6, 1, 4, 1}) {
		return nil
	}
	return append(codec.OID{}, oid[:7]...)
}

// objectFor turns one varbind into a neutral object, named by the MIB.
func (p *Plugin) objectFor(b codec.VarBind) dhsc.Object {
	o := dhsc.Object{
		OID:    b.Name.String(),
		Slot:   0,
		Access: 1, // read, until the MIB says otherwise
		Value:  toNeutral(b.Value),
	}
	o.Path = p.pathFor(b.Name)
	if len(o.Path) > 0 {
		o.Label = o.Path[len(o.Path)-1]
	}
	def, rest := mib.Compiled().Lookup(b.Name)
	if def == nil {
		o.Kind = o.Value.Kind
		return o
	}
	o.Value = named(def, o.Value)
	o.Kind = o.Value.Kind
	o.Label = def.Name
	if len(rest) > 0 {
		o.Label += "." + rest.String()
	}
	if accessIsWritable(def.Access) {
		o.Access |= 2
	}
	for _, e := range def.Enums {
		o.EnumItems = append(o.EnumItems, e.Name)
	}
	return o
}

// pathFor is the MIB's own hierarchy, made readable: every ancestor
// that the table names contributes one segment, with the parent's name
// stripped off the front of the child's (MIB convention is that a
// child repeats its parent — dr5000ChannelConfigurationInputSat under
// dr5000ChannelConfigurationInput), and the table index, if any, last.
//
//	1.3.6.1.4.1.27338.5.3.2.2.4.1.0
//	  → dr5000.ChannelConfiguration.Input.Sat.Interface
func (p *Plugin) pathFor(oid codec.OID) []string {
	t := mib.Compiled()
	var names []string
	for i := len(oid); i > 0; i-- {
		if def, rest := t.Lookup(oid[:i]); def != nil && len(rest) == 0 {
			names = append([]string{def.Name}, names...)
		}
	}
	if len(names) == 0 {
		return []string{oid.String()}
	}
	// Everything above the vendor's own root is scaffolding every OID
	// shares — org.dod.internet.private.enterprises, or …mgmt.mib-2.
	// A path starts where the device's own names start.
	for i, n := range names {
		if n == "enterprises" || n == "mib-2" {
			names = names[i+1:]
			break
		}
	}
	path := make([]string, 0, len(names)+1)
	prev := ""
	for _, n := range names {
		path = append(path, trimParent(prev, n))
		prev = n
	}
	// The instance: ".0" is the scalar's, and says nothing; a table row
	// index identifies the row and belongs in the path.
	if def, rest := t.Lookup(oid); def != nil && len(rest) > 0 && rest.String() != "0" {
		path = append(path, rest.String())
	}
	return path
}

// trimParent drops what a child's name repeats from its parent's. MIB
// names are built by concatenation, but not strictly — a table's Entry
// is "…AutomaticEntry" under "…AutomaticTable", sharing everything up
// to "Automatic" and not the parent's whole name. So the shared head is
// what goes, cut back to a word boundary so "…Satellite" under "…Sat"
// never becomes "ellite".
func trimParent(parent, name string) string {
	if parent == "" {
		return name
	}
	i := 0
	for i < len(parent) && i < len(name) && parent[i] == name[i] {
		i++
	}
	for i > 0 && i < len(name) && !isWordStart(name[i]) {
		i--
	}
	if i == 0 || i >= len(name) {
		return name
	}
	return name[i:]
}

// isWordStart reports whether a byte begins a word in a MIB name: an
// upper-case letter, or a digit that follows one.
func isWordStart(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// instanceOf appends the instance a GET needs. A scalar is read at
// ".0"; a column is read at a row index, which the caller must have
// named in the path (…Table.Entry.Pid.3), and which is already there.
func (p *Plugin) instanceOf(oid codec.OID) codec.OID {
	def, _ := mib.Compiled().Lookup(oid)
	if def != nil && def.Kind == "object-type" && len(def.Index) == 0 {
		return append(append(codec.OID{}, oid...), 0)
	}
	return oid
}

func accessIsWritable(a string) bool {
	switch strings.ToLower(a) {
	case "read-write", "read-create", "write-only":
		return true
	}
	return false
}

// GetValue reads one object. The request may name it three ways: the
// friendly path a walk produced, a MIB name ("sysDescr.0"), or a
// dotted OID — an operator should not have to remember which of those
// the connector prefers.
func (p *Plugin) GetValue(ctx context.Context, req dhsc.ValueRequest) (dhsc.Value, error) {
	sess, err := p.session()
	if err != nil {
		return dhsc.Value{}, err
	}
	oid, err := p.resolve(req)
	if err != nil {
		return dhsc.Value{}, err
	}
	binds, err := sess.Get(ctx, oid)
	if err != nil {
		return dhsc.Value{}, err
	}
	p.RecordRx()
	v, err := firstBind(binds, oid, "reply")
	if err != nil {
		return dhsc.Value{}, err
	}
	return p.decode(oid, v), nil
}

// SetValue writes one object and reads it back: the agent's echo is
// its own claim, and ADR-0030 does not trust one.
func (p *Plugin) SetValue(ctx context.Context, req dhsc.ValueRequest, val dhsc.Value) (dhsc.Value, error) {
	sess, err := p.writeSession(ctx)
	if err != nil {
		return dhsc.Value{}, err
	}
	read, err := p.session()
	if err != nil {
		return dhsc.Value{}, err
	}
	oid, err := p.resolve(req)
	if err != nil {
		return dhsc.Value{}, err
	}
	v, err := p.wireValue(oid, val)
	if err != nil {
		return dhsc.Value{}, err
	}
	if _, err := sess.Set(ctx, codec.VarBind{Name: oid, Value: v}); err != nil {
		return dhsc.Value{}, err
	}
	p.RecordTx()
	binds, err := read.Get(ctx, oid)
	if err != nil {
		return dhsc.Value{}, fmt.Errorf("snmp: set applied but confirm read failed: %w", err)
	}
	p.RecordRx()
	got, err := firstBind(binds, oid, "confirm read")
	if err != nil {
		return dhsc.Value{}, err
	}
	return p.decode(oid, got), nil
}

// wireValue turns a neutral value into the syntax the MIB declares for
// that object — an operator writes "rf1" or "1", not a type tag.
func (p *Plugin) wireValue(oid codec.OID, val dhsc.Value) (codec.Value, error) {
	def, _ := mib.Compiled().Lookup(oid)
	s := valueString(val)

	// An enum may be written by its name: the MIB is what maps it.
	if def != nil {
		for _, e := range def.Enums {
			if strings.EqualFold(e.Name, s) {
				return codec.Int(e.Value), nil
			}
		}
	}
	base := ""
	if def != nil {
		base = strings.ToLower(def.Base)
	}
	switch {
	case base == "octet string" || base == "displaystring" || val.Kind == dhsc.KindString && base == "":
		return codec.String(s), nil
	case base == "object identifier":
		o, err := codec.ParseOID(s)
		if err != nil {
			return codec.Value{}, err
		}
		return codec.ObjectID(o), nil
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return codec.Value{}, fmt.Errorf("snmp: %s is %s; %q is not a number and is no enum name", oid, base, s)
	}
	switch base {
	case "gauge32", "unsigned32":
		return codec.Gauge32(uint32(n)), nil
	case "counter32":
		return codec.Counter32(uint32(n)), nil
	case "timeticks":
		return codec.TimeTicks(uint32(n)), nil
	}
	return codec.Int(n), nil
}

func valueString(v dhsc.Value) string {
	switch v.Kind {
	case dhsc.KindInt:
		return strconv.FormatInt(v.Int, 10)
	case dhsc.KindUint:
		return strconv.FormatUint(v.Uint, 10)
	case dhsc.KindFloat:
		return strconv.FormatFloat(v.Float, 'g', -1, 64)
	case dhsc.KindBool:
		return strconv.FormatBool(v.Bool)
	}
	return v.Str
}

// SetCommunity takes the v1/v2c passwords. They are passwords, so they
// come from the environment like every other secret in this repo
// (SNMP_COMMUNITY / SNMP_WRITE_COMMUNITY) and never from a flag that
// would sit in a shell history and in ps.
func (p *Plugin) SetCommunity(read, write string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if read != "" {
		p.community = read
	}
	if write != "" {
		p.writeCmty = write
	}
}

// writeSession is the session a SET goes out on. An agent's write
// community is usually not its read one, and SNMP carries it in every
// PDU, so writing needs its own socket — opened on the first write and
// kept for the rest of the run.
func (p *Plugin) writeSession(ctx context.Context) (*Session, error) {
	p.mu.Lock()
	sess, cmty, host, port, ver := p.writeSess, p.writeCmty, p.host, p.port, p.version
	p.mu.Unlock()
	if sess != nil {
		return sess, nil
	}
	if cmty == "" {
		// No write community was given: the read session is what we
		// have, and the agent will say no if it is not enough.
		return p.session()
	}
	sess, err := Dial(ctx, Options{
		Addr: fmt.Sprintf("%s:%d", host, port), Version: ver, Community: cmty,
		Retries: DefaultRetries, Timeout: DefaultTimeout,
		Compliance: p.ComplianceProfile(),
	}, p.deps)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.writeSess = sess
	p.mu.Unlock()
	return sess, nil
}

// IdentityProbe answers "which card am I talking to" the way ADR-0022
// asks: Model@SwRev, so a per-model alarm template and a per-model DM
// find each other.
//
// SNMP has no standard object for either — sysDescr on this IRD is
// "Linux dp2 2.6.37", which says what it boots, not what it is. So the
// vendor's own branch is read by the convention every MIB in this lab
// follows: an object whose name ends in Model, and one whose name ends
// in a version. What comes back is the device's own words, and a
// device that publishes neither gets no identity rather than a guess.
func (p *Plugin) IdentityProbe(ctx context.Context, slot int) (string, error) {
	sess, err := p.session()
	if err != nil {
		return "", err
	}
	model := p.firstNamed(ctx, sess, "UnitModel", "Model", "UnitName", "ProductName")
	version := p.firstNamed(ctx, sess, "SoftwareCurrentVersion", "CurrentVersion",
		"SoftwareVersion", "FirmwareVersion", "Version")
	switch {
	case model != "" && version != "":
		return model + "@" + version, nil
	case model != "":
		return model, nil
	}
	return "", nil
}

// firstNamed reads the first object under the device's own branch
// whose MIB name ends in one of the suffixes, in the order given.
func (p *Plugin) firstNamed(ctx context.Context, sess *Session, suffixes ...string) string {
	p.mu.Lock()
	root := p.sysOID
	p.mu.Unlock()
	if len(root) < 7 || !root.HasPrefix(codec.OID{1, 3, 6, 1, 4, 1}) {
		return ""
	}
	under := mib.Compiled().Under(append(codec.OID{}, root[:7]...))
	for _, suffix := range suffixes {
		for _, o := range under {
			if o.Kind != "object-type" || !strings.HasSuffix(o.Name, suffix) || len(o.Index) != 0 {
				continue
			}
			binds, err := sess.Get(ctx, p.instanceOf(o.OID))
			if err != nil || len(binds) == 0 || binds[0].Value.Type != codec.TypeOctetString {
				continue
			}
			if v := strings.TrimSpace(string(binds[0].Value.Bytes)); v != "" {
				return v
			}
		}
	}
	return ""
}

// MinOpTimeout is how long one exchange may honestly take here. UDP
// loses datagrams, so a read is one attempt plus DefaultRetries, each
// bounded by DefaultTimeout; the CLI's 1 s default is shorter than
// that, and a device that answers in 700 ms then reads as "context
// deadline exceeded". The floor is the retry budget plus a margin for
// the walk of round trips a GET of six varbinds makes.
func (p *Plugin) MinOpTimeout() time.Duration {
	return time.Duration(DefaultRetries+1)*DefaultTimeout + 4*time.Second
}

// PathNative says the plugin resolves a path itself: the MIB is the
// map, so the CLI must not walk 20 000 objects to find one name.
func (p *Plugin) PathNative() bool { return true }

// pathIndex maps the friendly paths of one branch to their OIDs,
// straight from the compiled MIB — so `get --path` needs no walk, and
// a device that has never been walked still answers by name.
func (p *Plugin) pathIndex() map[string]codec.OID {
	p.mu.Lock()
	idx, root := p.paths, p.sysOID
	p.mu.Unlock()
	if idx != nil {
		return idx
	}
	idx = map[string]codec.OID{}
	branches := []codec.OID{systemGroup}
	if len(root) >= 7 && root.HasPrefix(codec.OID{1, 3, 6, 1, 4, 1}) {
		branches = append(branches, append(codec.OID{}, root[:7]...))
	}
	for _, b := range branches {
		for _, o := range mib.Compiled().Under(b) {
			// Leaves AND branches: a leaf is what `get` names, a branch
			// is what `watch --path` scopes to.
			if o.Kind != "object-type" && o.Kind != "object-identifier" {
				continue
			}
			key := strings.Join(p.pathFor(o.OID), ".")
			if _, dup := idx[key]; !dup {
				idx[key] = o.OID
			}
			// The MIB's own name works too: nobody types
			// "system.Location" when they mean sysLocation.
			if _, dup := idx[o.Name]; !dup {
				idx[o.Name] = o.OID
			}
		}
	}
	p.mu.Lock()
	p.paths = idx
	p.mu.Unlock()
	return idx
}

// resolve turns whatever the request names into an OID.
func (p *Plugin) resolve(req dhsc.ValueRequest) (codec.OID, error) {
	name := strings.TrimSpace(req.Path)
	if name == "" {
		name = strings.TrimSpace(req.Label)
	}
	if name == "" {
		return nil, fmt.Errorf("snmp: name the object with --path (a walked path, a MIB name, or an OID)")
	}
	p.mu.Lock()
	oid, walked := p.byPath[name]
	p.mu.Unlock()
	if walked {
		return oid, nil
	}
	if oid, ok := p.pathIndex()[name]; ok {
		return p.instanceOf(oid), nil
	}
	// A number is a number: an OID typed in full names an instance the
	// MIB may not have a row for (a table cell, a vendor's undocumented
	// leaf), and refusing it would make the connector less capable than
	// the wire it speaks.
	if oid, err := codec.ParseOID(name); err == nil {
		return oid, nil
	}
	return mib.Compiled().Resolve(name)
}

// Subscribe polls: SNMP notifications are the agent's choice, not the
// manager's, so a watch that waits for traps watches nothing. The
// ADR-0030 monitor does the polling, one entry per walked leaf.
func (p *Plugin) Subscribe(req dhsc.ValueRequest, fn dhsc.EventFunc) error {
	if _, err := p.session(); err != nil {
		return err
	}
	return p.poller.Subscribe(req, fn)
}

// Unsubscribe stops that poll.
func (p *Plugin) Unsubscribe(req dhsc.ValueRequest) error { return p.poller.Unsubscribe(req) }

// SetPollInterval sets how often a watch re-reads each object. An
// agent has no dictionary to take intervals from, so the operator's
// `--interval` is the whole plan.
func (p *Plugin) SetPollInterval(d time.Duration) {
	if d <= 0 {
		return
	}
	p.mu.Lock()
	p.interval = d
	p.mu.Unlock()
}

// pollProfileFor builds the poll plan from the last walk, narrowed to
// the request's --path scope. Values are judged per sample (a counter
// that stopped, a link still down), so the plan reports every sample
// and lets the display skip the repeats.
func (p *Plugin) pollProfileFor(ctx context.Context, req dhsc.ValueRequest) (*monitor.Profile, error) {
	p.mu.Lock()
	tree, interval := p.tree, p.interval
	p.mu.Unlock()

	filter := strings.TrimSpace(req.Path)

	// A scope is a subtree, and walking the whole device to find it
	// would be absurd: an IRD publishes a 4096-row programme table
	// that a --path never asks about. Resolve the scope first and walk
	// only that branch.
	if filter != "" {
		if root, err := p.resolve(dhsc.ValueRequest{Path: filter}); err == nil {
			scoped, werr := p.walkUnder(ctx, p.branchOf(root))
			if werr != nil {
				return nil, werr
			}
			tree = scoped
		}
	}
	if len(tree) == 0 {
		var err error
		if tree, err = p.Walk(ctx, 0); err != nil {
			return nil, err
		}
	}
	onChange := false
	prof := &monitor.Profile{
		Defaults: monitor.Defaults{Interval: monitor.Duration(interval), OnChange: true},
	}
	for _, o := range tree {
		path := strings.Join(o.Path, ".")
		if filter != "" && path != filter && !strings.HasPrefix(path, filter+".") {
			continue
		}
		prof.Entries = append(prof.Entries, monitor.Entry{
			Path: path, Slot: 0, OID: o.OID,
			Interval: monitor.Duration(interval), OnChange: &onChange,
		})
	}
	if len(prof.Entries) == 0 {
		sample := ""
		if len(tree) > 0 {
			sample = strings.Join(tree[len(tree)-1].Path, ".")
		}
		return nil, fmt.Errorf("snmp: nothing to poll under %q (%d object(s) walked, e.g. %q)",
			filter, len(tree), sample)
	}
	return prof, nil
}

// WalkUnder reads one branch of the model, named the way the CLI names
// it. A device with 26 000 objects is not walked whole to look at its
// input status, and an operator should not have to know an OID to say
// so.
func (p *Plugin) WalkUnder(ctx context.Context, path string) ([]dhsc.Object, error) {
	oid, err := p.resolve(dhsc.ValueRequest{Path: path})
	if err != nil {
		return nil, err
	}
	return p.walkUnder(ctx, p.branchOf(oid))
}

// walkOutcome decides what a branch that stopped means. Nothing read
// at all is fatal — the caller asked for a model and there is none.
// Part of a model in hand is a warning, because silently returning six
// objects is how a device with 26 000 looks fine. Hitting the cap is
// always a warning: the objects are real, there are simply more.
func walkOutcome(objs int, err, stop error) (warn bool, fatal error) {
	switch {
	case err == nil:
		return false, nil
	case err == stop:
		return true, nil
	case objs == 0:
		return false, err
	default:
		return true, nil
	}
}

// oidFromBinds reads an OBJECT IDENTIFIER out of a reply. An agent
// that answers sysObjectID with something else has not said what it
// is, and guessing from the wrong syntax is worse than not knowing.
func oidFromBinds(binds []codec.VarBind) codec.OID {
	if len(binds) == 0 || binds[0].Value.Type != codec.TypeOID {
		return nil
	}
	return binds[0].Value.OID
}

// branchOf drops a scalar's ".0" so a subtree walk starts at the node
// itself and not past it.
func (p *Plugin) branchOf(oid codec.OID) codec.OID {
	if n := len(oid); n > 0 && oid[n-1] == 0 {
		return oid[:n-1]
	}
	return oid
}

// walkUnder reads one subtree into neutral objects.
func (p *Plugin) walkUnder(ctx context.Context, root codec.OID) ([]dhsc.Object, error) {
	sess, err := p.session()
	if err != nil {
		return nil, err
	}
	var objs []dhsc.Object
	errStop := fmt.Errorf("snmp: walk limit")
	werr := sess.Walk(ctx, root, func(b codec.VarBind) error {
		if len(objs) >= walkLimit {
			return errStop
		}
		objs = append(objs, p.objectFor(b))
		return nil
	})
	p.RecordRx()
	if werr != nil && werr != errStop && len(objs) == 0 {
		return nil, werr
	}
	return objs, nil
}

// firstBind is the value a reply carries for one name. An agent that
// answers with no varbind at all has said nothing, and saying nothing
// is not a value — a caller that took binds[0] blindly would panic on
// exactly the agent that is already misbehaving.
func firstBind(binds []codec.VarBind, oid codec.OID, what string) (codec.Value, error) {
	if len(binds) == 0 {
		return codec.Value{}, fmt.Errorf("snmp: %s: no varbind in the %s", oid, what)
	}
	return binds[0].Value, nil
}

// decode is toNeutral plus the MIB: an INTEGER the module declared as
// an enumeration comes back as the name the device's own manual uses
// ("rf1", "true", "OK"), because that is what an operator reads and
// what an alarm rule is written against.
func (p *Plugin) decode(oid codec.OID, v codec.Value) dhsc.Value {
	def, _ := mib.Compiled().Lookup(oid)
	return named(def, toNeutral(v))
}

// named applies an object's enumeration to a value.
func named(def *mib.Object, v dhsc.Value) dhsc.Value {
	if def == nil || len(def.Enums) == 0 || v.Kind != dhsc.KindInt {
		return v
	}
	for _, e := range def.Enums {
		if e.Value == v.Int {
			return dhsc.Value{Kind: dhsc.KindEnum, Enum: uint8(v.Int), Str: e.Name}
		}
	}
	return v
}

// toNeutral maps an SNMP value onto the neutral one. The monitor
// adapter has the same mapping for its own use; both are one switch
// over the SMI types, and neither package is the other's dependency.
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
	}
	return dhsc.Value{Kind: dhsc.KindUnknown}
}
