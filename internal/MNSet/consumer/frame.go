package mnset

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	stdhttp "net/http"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/consumer"
)

// Frame mode — the ADR-0022 shape the other connectors have: a frame
// with a controller and cards in slots. Here the controller is MN SET
// (its /api/device list) and every module it manages is one slot; the
// data still comes from each module directly. A module addressed on its
// own (no MN SET) is the one-slot case, slot 0.
//
// Slot numbering: MN SET has none, and its list order is not stable, so
// slots are the modules sorted by MN SET's device id (the module MAC),
// which is stable for the life of the hardware. `info` prints the map.

// FramePort is MN SET's application port.
const FramePort = 8080

// frameMaxBody bounds the /api/device answer: one module is ~120 KiB of
// JSON (44 keys, every flow), so a hundred modules fit in 64 MiB.
const frameMaxBody = 64 << 20

// slotModule is one module as a slot of the frame.
type slotModule struct {
	id     string // MN SET device id = module MAC
	ip     string // media address (interfaces.e1.current_ip)
	status string // ONLINE / OFFLINE as MN SET reports it
	typ    string
	serial string
	lldp   string
	c      *client           // nil when the module is not reachable
	info   any               // self/information when reachable
	cages  map[string]string // "cage3" → what is fitted, from port/<n>
}

// readCages lists the module's SFP cages (port/<n>) and renders what is
// fitted in each — part number, serial, pinout, link, speed — so `info`
// shows a module the way it shows a frame: slot, card, serial. A module
// that hides port/ simply has no cages listed.
func readCages(ctx context.Context, c *client) map[string]string {
	doc, err := c.get(ctx, "port")
	if err != nil {
		return nil
	}
	names, ok := listing(doc)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(names))
	for _, n := range names {
		pd, err := c.get(ctx, "port/"+n)
		if err != nil {
			continue
		}
		m, ok := pd.(map[string]any)
		if !ok {
			continue
		}
		part, serial := str(m["detected_sfp_part_number"]), str(m["detected_sfp_serial_number"])
		fitted := "empty"
		if part != "" && part != "N/A" {
			fitted = part + " sn " + serial
		}
		out["cage"+n] = fmt.Sprintf("%s · %s · link %s %s", fitted, str(m["host_pinout"]), str(m["link"]), str(m["speed"]))
	}
	return out
}

// connectFrame reads MN SET's device list and opens every ONLINE module.
// An ONLINE module that does not answer stays a slot (SlotError) so the
// operator sees it; an OFFLINE one is SlotNoCard.
func (p *Plugin) connectFrame(ctx context.Context, ip string, port int) ([]slotModule, error) {
	base := "http://" + net.JoinHostPort(ip, strconv.Itoa(port))
	hc := &stdhttp.Client{Timeout: p.timeout, Transport: p.Transport}
	raw, status, err := mnsetCall(ctx, hc, stdhttp.MethodGet, base+"/api/device", "", "")
	if err != nil {
		return nil, fmt.Errorf("mnset frame %s: device list: %w", ip, err)
	}
	if status == stdhttp.StatusUnauthorized || status == stdhttp.StatusForbidden {
		// This deployment serves /api/device without a token; one that
		// does not gets the operator's login (never logged).
		if p.user == "" {
			return nil, fmt.Errorf("mnset frame %s: device list refused (http %d) and no credentials set (MNSET_USER / MNSET_PASS)", ip, status)
		}
		token, err := login(ctx, hc, base, p.user, p.pass)
		if err != nil {
			return nil, fmt.Errorf("mnset frame %s: %w", ip, err)
		}
		if raw, status, err = mnsetCall(ctx, hc, stdhttp.MethodGet, base+"/api/device", "", token); err != nil {
			return nil, fmt.Errorf("mnset frame %s: device list: %w", ip, err)
		}
	}
	if status != stdhttp.StatusOK {
		return nil, fmt.Errorf("mnset frame %s: device list: http %d", ip, status)
	}
	var devices []map[string]any
	if err := json.Unmarshal(raw, &devices); err != nil {
		return nil, fmt.Errorf("mnset frame %s: device list is not a JSON array: %w", ip, err)
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("mnset frame %s: MN SET manages no module", ip)
	}

	slots := make([]slotModule, 0, len(devices))
	for _, d := range devices {
		info, _ := d["info"].(map[string]any)
		s := slotModule{
			id: str(d["id"]), status: str(d["status"]),
			typ: str(info["type"]), serial: str(info["serial_number"]),
			lldp: strings.ReplaceAll(str(d["lldpLocation"]), "\n", " "),
		}
		if ifs, ok := d["interfaces"].(map[string]any); ok {
			if e1, ok := ifs["e1"].(map[string]any); ok {
				s.ip = strings.SplitN(str(e1["current_ip"]), "/", 2)[0]
			}
		}
		slots = append(slots, s)
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].id < slots[j].id })

	for i := range slots {
		s := &slots[i]
		if s.status != "ONLINE" || s.ip == "" {
			continue
		}
		c := newClient(s.ip, p.modulePort, p.timeout, p.Transport, p.Metrics(), p.Recorder())
		info, err := c.get(ctx, "self/information")
		if err != nil {
			p.logger.Warn("mnset frame: module listed ONLINE but not answering",
				"slot", i, "id", s.id, "ip", s.ip, "err", err.Error())
			continue
		}
		s.c, s.info, s.cages = c, info, readCages(ctx, c)
	}
	return slots, nil
}

// login is MN SET's own: the username in the path, the password as a
// raw text body, the token returned as JSON. Password and token are
// never logged.
func login(ctx context.Context, hc *stdhttp.Client, base, user, pass string) (string, error) {
	raw, status, err := mnsetCall(ctx, hc, stdhttp.MethodPost, base+"/api/authentication/login/"+user, pass, "")
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	var l struct {
		Token   string `json:"token"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &l); err != nil || l.Token == "" {
		if l.Message != "" {
			return "", fmt.Errorf("login refused: %s", l.Message)
		}
		return "", fmt.Errorf("login: no token in answer (http %d)", status)
	}
	return l.Token, nil
}

// slotInfoOf renders one frame slot.
func slotInfoOf(n int, s slotModule) consumer.SlotInfo {
	si := consumer.SlotInfo{Slot: n, Identity: map[string]string{
		"id": s.id, "ip": s.ip, "type": s.typ, "serial": s.serial, "lldp": s.lldp, "mnset_status": s.status,
	}}
	for k, v := range s.cages {
		si.Identity[k] = v
	}
	switch {
	case s.c != nil:
		si.Status, si.IsOnline = consumer.SlotPresent, true
		si.Identity["identity"] = identityOf(s.info)
	case s.status == "ONLINE":
		si.Status = consumer.SlotError // MN SET says up, the module does not answer
	default:
		si.Status = consumer.SlotNoCard
	}
	si.State = si.Status.State()
	return si
}
