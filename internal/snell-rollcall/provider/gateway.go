package rollcall

import (
	"fmt"
	"net"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"

	"dhs/internal/snell-rollcall/codec"
)

// The gateway is a unit like any other and a panel expects to open it.
//
// Ours had no menu at all, so selecting it in a Control Panel offered nothing:
// no template, no lines, nothing to read. A real controller does the opposite —
// the Nucleus template that ships with the Centra simulator draws a Unit Setup
// page of exactly this shape, with the controller's type and state, its IP
// address, its IPShare port and its software and firmware versions.
//
// What a connector can honestly show is what it is doing: where it is
// listening, which generation it offers, how many cards it serves, and how
// many spec deviations it has absorbed.

// gatewayCommand numbers the gateway's own lines. They are its commands, not a
// card's, and they start at one like any other unit's.
const (
	cmdGatewayAddress    uint32 = 1
	cmdGatewayPort       uint32 = 2
	cmdGatewayUnit       uint32 = 3
	cmdGatewayGeneration uint32 = 4
	cmdGatewayCards      uint32 = 5
	cmdGatewayVersion    uint32 = 6
	cmdGatewayBuild      uint32 = 7
	cmdGatewayBuilt      uint32 = 8
	cmdGatewayGo         uint32 = 9
	cmdGatewayProtocol   uint32 = 10
	cmdGatewayUptime     uint32 = 11
	cmdGatewayEvents     uint32 = 12
)

// newGatewayPort builds the menu a gateway serves about itself.
//
// Every line is read-only. A connector that let a panel change where it listens
// would be answering the question by cutting the wire, and the one control
// worth having — turning logging up — needs a logger whose level can move at
// runtime, which this one does not have yet.
func newGatewayPort(id codec.ID) *port {
	p := &port{
		number: 0,
		id:     id,
		byCmd:  make(map[uint32]int),
		byPath: make(map[string]int),
		values: make(map[uint32]codec.Value),
	}

	add := func(text, path string, style codec.Style, cmd uint32, min, max int32) {
		p.lines = append(p.lines, line{
			Index:    uint32(len(p.lines)),
			Style:    style,
			Command:  cmd,
			MinRange: min,
			MaxRange: max,
			Text:     text,
			path:     path,
		})
		if cmd != 0 {
			p.byCmd[cmd] = len(p.lines) - 1
			p.byPath[path] = len(p.lines) - 1
		}
	}

	str := codec.StyleEditString | codec.StyleDisabled
	num := codec.StyleNumber | codec.StyleDisabled

	add("Ethernet", "ethernet", codec.StyleList, 0, 0, 0)
	add("IP Address", "ethernet.address", str, cmdGatewayAddress, 0, codec.MaxLongString-1)
	add("IPShare Port", "ethernet.port", num, cmdGatewayPort, 0, 65535)

	add("RollCall", "rollcall", codec.StyleList, 0, 0, 0)
	add("Unit", "rollcall.unit", num, cmdGatewayUnit, 0, 255)
	add("Generation", "rollcall.generation", str, cmdGatewayGeneration, 0, codec.MaxLongString-1)
	add("Cards", "rollcall.cards", num, cmdGatewayCards, 0, 255)

	add("Software", "software", codec.StyleList, 0, 0, 0)
	add("Version", "software.version", str, cmdGatewayVersion, 0, codec.MaxLongString-1)
	add("Build", "software.build", str, cmdGatewayBuild, 0, codec.MaxLongString-1)
	add("Built", "software.built", str, cmdGatewayBuilt, 0, codec.MaxLongString-1)
	add("Go", "software.go", str, cmdGatewayGo, 0, codec.MaxLongString-1)

	add("Status", "status", codec.StyleList, 0, 0, 0)
	add("Protocol", "status.protocol", str, cmdGatewayProtocol, 0, codec.MaxLongString-1)
	add("Uptime", "status.uptime", str, cmdGatewayUptime, 0, codec.MaxLongString-1)
	add("Compliance Events", "status.events", num, cmdGatewayEvents, 0, 0x7FFFFFFF)

	return p
}

// refreshGateway fills the gateway's own lines with what is true now.
//
// It is called when the listener binds and whenever a client reads them, so a
// panel showing the page sees the connector as it is rather than as it was
// when the tree was loaded.
func (p *Provider) refreshGateway() {
	gw := p.model.port(0)
	if gw == nil {
		return
	}

	p.mu.RLock()
	addr := p.addr
	unit := p.unit
	p.mu.RUnlock()

	host, port := splitListen(addr)
	generation := "16-bit"
	if p.served().LongStrings() {
		generation = "32-bit"
	}

	text := func(cmd uint32, v string) {
		gw.seed(cmd, codec.Value{Mode: codec.ModeString, Text: v})
	}
	number := func(cmd uint32, v int32) {
		gw.seed(cmd, codec.Value{Mode: codec.ModeValue, Val: v})
	}

	text(cmdGatewayAddress, host)
	number(cmdGatewayPort, int32(port))
	number(cmdGatewayUnit, int32(unit))
	text(cmdGatewayGeneration, generation)
	number(cmdGatewayCards, int32(len(p.model.portNumbers())))
	b := buildFacts()
	text(cmdGatewayVersion, b.version)
	text(cmdGatewayBuild, b.commit)
	text(cmdGatewayBuilt, b.built)
	text(cmdGatewayGo, b.goVersion)

	text(cmdGatewayProtocol, fmt.Sprintf("RollCall v%d", codec.ProtocolVersion))
	text(cmdGatewayUptime, p.uptime())
	number(cmdGatewayEvents, int32(len(p.ComplianceEvents())))
}

// facts is what the binary can say about itself.
type facts struct {
	version   string
	commit    string
	built     string
	goVersion string
}

// buildFacts reads the module's own build information.
//
// It comes from the binary rather than from a constant, so a page cannot claim
// a version the code is not. A binary built outside a module says so instead of
// inventing a number.
func buildFacts() facts {
	info, _ := debug.ReadBuildInfo()
	return factsFrom(info)
}

// factsFrom reads what a build record says, apart from the reading of it, so
// the shapes a build record can take are testable without producing them.
func factsFrom(info *debug.BuildInfo) facts {
	f := facts{version: "devel", commit: "unknown", built: "unknown", goVersion: runtime.Version()}
	if info == nil {
		// A binary with no build record says what it knows, which is the Go
		// version it was compiled by and nothing else.
		return f
	}

	if v := info.Main.Version; v != "" && v != "(devel)" {
		f.version = v
	}
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				f.commit = s.Value[:7]
			} else if s.Value != "" {
				f.commit = s.Value
			}
		case "vcs.time":
			f.built = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if modified && f.commit != "unknown" {
		// A build with uncommitted changes is not the commit it names, and an
		// operator reading the page should be told so.
		f.commit += "-dirty"
	}
	return f
}

// uptime is how long this provider has been running, as a person reads it.
func (p *Provider) uptime() string {
	p.mu.RLock()
	started := p.started
	p.mu.RUnlock()

	if started.IsZero() {
		return "not started"
	}
	d := p.clk.Now().Sub(started).Round(time.Second)
	return d.String()
}

// splitListen pulls the host and port out of a listen address, which is empty
// until the listener binds.
func splitListen(addr string) (string, int) {
	if addr == "" {
		return "not listening", 0
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, 0
	}
	if host == "" || host == "::" {
		// A wildcard bind answers on every address it has; saying so is more
		// use to somebody reading the page than an empty field.
		host = "all interfaces"
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return host, 0
	}
	return host, port
}
