package rollcall

import (
	"net"
	"strconv"

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
	cmdGatewayEvents     uint32 = 7
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
	add("Compliance Events", "software.events", num, cmdGatewayEvents, 0, 0x7FFFFFFF)

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
	text(cmdGatewayVersion, gatewayVersion)
	number(cmdGatewayEvents, int32(len(p.ComplianceEvents())))
}

// gatewayVersion is what the connector calls itself on its own page.
const gatewayVersion = "dhs snell-rollcall"

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
