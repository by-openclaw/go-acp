package rollcall

import (
	"sort"

	"dhs/internal/snell-rollcall/codec"
)

// handle answers one request the way a RollCall server would.
//
// It is deliberately a server rather than a script of expected messages: a
// test that drives it exercises the same bytes a device would see, so a wrong
// field is a failure rather than a rewritten expectation.
func (d *device) handle(f codec.Frame) {
	d.mu.Lock()
	refuse, garble, silent := d.refuse[f.Type], d.garble[f.Type], d.silent[f.Type]
	d.mu.Unlock()

	if silent {
		// Saying nothing is not the same as refusing, and the difference is
		// the whole of issue #1042: the vendor proxy answers a device enquiry
		// and then ignores a port list, so a client that treats silence as
		// fatal waits out its own deadline instead of asking the map.
		return
	}
	if refuse {
		d.reply(f, codec.MsgNack, []byte{'r', 'e', 'f', 'u', 's', 'e', 'd', 0})
		return
	}
	if garble {
		// A payload too short for the structure it claims to be. A device
		// with a firmware fault looks exactly like this from outside.
		d.reply(f, replyTypeFor(f.Type), []byte{0x01})
		return
	}

	switch f.Type {
	case codec.MsgGetDevInfo:
		d.handshake(f)
	case codec.MsgCall:
		d.call(f)
	case codec.MsgTerm:
		d.term(f)
	case codec.MsgIam:
		// An announcement is never answered: it is broadcast, and a peer that
		// replied would be talking to everybody. The device records it so a
		// test can prove the client said it was here.
		d.recordIam(f)
	case codec.MsgKeepAlive:
		d.reply(f, codec.MsgAck, nil)

	case codec.MsgGetID:
		d.getID(f)
	case codec.MsgGetStat:
		d.getStat(f)
	case codec.MsgGetDevList:
		d.deviceList(f)
	case codec.MsgGetLocDevMap:
		d.deviceMap(f)

	case codec.MsgGetMenuCount:
		d.menuCount(f)
	case codec.MsgGetMenuItem:
		d.menuItem(f)
	case codec.MsgGetFunc:
		d.menuBlock(f)
	case codec.MsgGetNextPkt:
		d.menuNext(f)

	case codec.MsgGetValue:
		d.getValue(f)
	case codec.MsgSetValue:
		d.setValueMsg(f)
	case codec.MsgGetFStat:
		d.getFStat(f)
	case codec.MsgSetParam:
		d.setParam(f)

	case codec.MsgGetDispData:
		d.dispData(f)
	case codec.MsgBkChnReady:
		d.backChannelReady(f)
	case codec.MsgRepFChg:
		d.reply(f, codec.MsgAck, nil)

	case codec.MsgFileOpen:
		d.fileOpen(f)
	case codec.MsgFileRead:
		d.fileRead(f)
	case codec.MsgFileDir:
		d.fileDir(f)
	case codec.MsgFileClose:
		d.reply(f, codec.MsgFileRet, codec.File{}.AppendTo(nil))

	case codec.MsgAck:
		// An acknowledgement of one of our pushes. Nothing to do.

	default:
		// A server answers what it does not implement with InvCmd, which is
		// different from refusing: it means "I do not know this message".
		d.reply(f, codec.MsgInvCmd, nil)
	}
}

// replyTypeFor names the answer a request expects, so a garbled reply is the
// right type with the wrong contents rather than an obviously wrong message.
func replyTypeFor(req codec.PacketType) codec.PacketType {
	switch req {
	case codec.MsgGetDevInfo, codec.MsgGetDevList, codec.MsgGetLocDevMap:
		return codec.MsgRetDevInfo
	case codec.MsgGetID:
		return codec.MsgRetID
	case codec.MsgGetStat:
		return codec.MsgRetStat
	case codec.MsgGetMenuCount:
		return codec.MsgRetMenuCount
	case codec.MsgGetMenuItem:
		return codec.MsgRetMenuItem
	case codec.MsgGetFunc:
		return codec.MsgBlockHeader
	case codec.MsgGetNextPkt:
		return codec.MsgRetFunc
	case codec.MsgGetValue, codec.MsgSetValue:
		return codec.MsgRetValue
	case codec.MsgGetFStat, codec.MsgSetParam:
		return codec.MsgRetFStat
	case codec.MsgGetDispData:
		return codec.MsgDispData
	case codec.MsgFileOpen:
		return codec.MsgRetFileOpen
	case codec.MsgFileRead:
		return codec.MsgRetFileRead
	case codec.MsgFileDir:
		return codec.MsgBlockHeader
	default:
		return codec.MsgAck
	}
}

func (d *device) handshake(f codec.Frame) {
	d.mu.Lock()
	services := d.services
	d.mu.Unlock()

	info := codec.DeviceInfo{
		ProtocolVersion: codec.ProtocolVersion,
		Address:         gatewayAddr,
		ID: codec.ID{
			Services: services,
			TypeID:   636,
			Version:  codec.Version{Major: 1, Minor: 2, Alpha: ' ', CmdSet: 1},
			Name:     "Centra",
		},
		Status: codec.UnitStatus{Status: codec.StatusPresent | codec.StatusOnline},
	}
	payload, err := info.AppendTo(nil)
	if err != nil {
		d.fail("device: encode device info: %v", err)
		return
	}

	// The gateway stamps the client's address into the destination, which is
	// how a TCP client learns what it is called.
	d.send(codec.Frame{
		Dst:     codec.Address{Unit: 0, Port: 0xE0, Index: f.Src.Index},
		Src:     gatewayAddr,
		Type:    codec.MsgRetDevInfo,
		Payload: payload,
	})
}

func (d *device) call(f codec.Frame) {
	d.mu.Lock()
	d.callsSeen++
	gate := d.gateCall
	d.mu.Unlock()

	// A held call waits off the read loop. Blocking the loop instead would
	// stop the second caller's request ever being read, which is the race the
	// gate exists to create.
	if gate != nil {
		go func() {
			<-gate
			d.answerCall(f)
		}()
		return
	}
	d.answerCall(f)
}

func (d *device) answerCall(f codec.Frame) {
	conn, err := codec.DecodeConnect(f.Payload)
	if err != nil {
		d.reply(f, codec.MsgNack, nil)
		return
	}

	d.mu.Lock()
	if d.refuseSessionOn >= 0 && int(f.Dst.Port) == d.refuseSessionOn {
		// Says nothing at all, which is what a client does when another
		// client asks it for a session.
		d.mu.Unlock()
		return
	}
	refuse := d.refuseLongStrings && conn.Services.LongStrings()
	if refuse {
		d.mu.Unlock()
		d.reply(f, codec.MsgNack, []byte("no long strings\x00"))
		return
	}
	if d.refusePorts && conn.Services.Has(codec.SvcPorts) {
		d.mu.Unlock()
		d.reply(f, codec.MsgNack, append([]byte("no port service"), 0))
		return
	}
	if d.refuseMap && conn.Services.Has(codec.SvcMap) {
		d.mu.Unlock()
		d.reply(f, codec.MsgNack, append([]byte("no map service"), 0))
		return
	}
	idx := d.nextIdx
	d.nextIdx++
	d.sessions[f.Src.Index] = f.Dst.Port
	d.sessionSvc[f.Src.Index] = conn.Services
	d.calls++
	d.mu.Unlock()

	d.send(codec.Frame{
		Dst:  f.Src,
		Src:  codec.Address{Unit: gatewayAddr.Unit, Port: f.Dst.Port, Index: idx},
		Type: codec.MsgAck,
	})
}

func (d *device) term(f codec.Frame) {
	d.mu.Lock()
	delete(d.sessions, f.Src.Index)
	delete(d.backChannel, f.Src.Index)
	d.mu.Unlock()
}

// recordIam keeps what a client announced about itself.
func (d *device) recordIam(f codec.Frame) {
	info, err := codec.DecodeDeviceInfo(f.Payload)
	if err != nil {
		return
	}
	d.mu.Lock()
	d.iams = append(d.iams, info)
	d.mu.Unlock()
	select {
	case d.iamSeen <- struct{}{}:
	default:
	}
}

// announced returns what the client has said about itself so far.
func (d *device) announced() []codec.DeviceInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]codec.DeviceInfo(nil), d.iams...)
}

// callCount is how many sessions have been opened, so a test can prove one was
// kept rather than reopened.
func (d *device) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// negotiated is what a session asked for.
func (d *device) negotiated(f codec.Frame) codec.Service {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sessionSvc[f.Src.Index]
}

// port returns which port a session was opened on.
func (d *device) port(f codec.Frame) uint8 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sessions[f.Src.Index]
}

// idOf is what a port reports about itself. The port list and the card's own
// answer come from here alike, because on real hardware they agree: the IQ
// frame's list carries exactly the name, type and version its Control Panel
// displays.
func (d *device) idOf(port uint8) codec.ID {
	d.mu.Lock()
	defer d.mu.Unlock()
	if id, ok := d.identity[port]; ok {
		return id
	}
	return codec.ID{
		Services: codec.SvcMenus | codec.SvcControl,
		TypeID:   623,
		Version:  codec.Version{Major: 2, Minor: 1, Alpha: 'a', CmdSet: 3},
		Name:     "5915 Card",
	}
}

// statusOf is the same for a port's status.
func (d *device) statusOf(port uint8) codec.UnitStatus {
	d.mu.Lock()
	empty := d.emptySlots[port]
	d.mu.Unlock()

	if empty {
		// A slot with nothing fitted. The unit answers rather than refusing:
		// "no card" is an answer, and a client walking a frame needs one per
		// slot.
		return codec.UnitStatus{Status: codec.StatusOnline}
	}
	return codec.UnitStatus{Status: codec.StatusPresent | codec.StatusOnline}
}

func (d *device) getID(f codec.Frame) {
	id := d.idOf(d.port(f))

	payload, err := id.AppendTo(nil)
	if err != nil {
		d.fail("device: encode id: %v", err)
		return
	}
	d.reply(f, codec.MsgRetID, payload)
}

func (d *device) getStat(f codec.Frame) {
	st := d.statusOf(f.Dst.Port)
	d.reply(f, codec.MsgRetStat, st.AppendTo(nil))
}

// deviceList answers a port enumeration with one entry per port.
//
// Each entry advertises what that node serves, which is what a real unit does:
// the vendor Centra's own list gives each node its own service mask, and a
// client uses it to decide what to ask that node for.
func (d *device) deviceList(f codec.Frame) {
	// A unit that implements the port service properly answers a port list
	// only on a session that negotiated it, and ignores the request otherwise
	// rather than refusing it. A real IQ 3U frame does exactly this.
	d.mu.Lock()
	strict := d.silentUnlessPorts
	d.mu.Unlock()
	if strict && !d.negotiated(f).Has(codec.SvcPorts) {
		return
	}

	d.mu.Lock()
	n := d.ports
	odd := d.oddListItem
	serviceless := d.servicelessPort
	services := d.services
	d.mu.Unlock()

	d.block(f, codec.MsgGetDevList, n, func(i int) (codec.PacketType, []byte) {
		if i == odd {
			return codec.MsgAck, nil
		}
		id := d.idOf(uint8(i))
		id.Services = services
		if i == serviceless {
			// A node that serves nothing, the way a frame lists an attached
			// Control Panel: it is in the list and it offers no service at all.
			id.Services = 0
		}
		info := codec.DeviceInfo{
			ProtocolVersion: codec.ProtocolVersion,
			Address:         codec.Address{Unit: gatewayAddr.Unit, Port: uint8(i), Index: codec.IndexUnknown},
			ID:              id,
			Status:          d.statusOf(uint8(i)),
		}
		payload, _ := info.AppendTo(nil)
		return codec.MsgRetDevInfo, payload
	})
}

func (d *device) deviceMap(f codec.Frame) {
	d.mu.Lock()
	empty := d.emptyDeviceMap
	d.mu.Unlock()

	count := 1
	if empty {
		count = 0
	}
	odd := d.oddListItem
	bad := d.badMapEntry
	d.block(f, codec.MsgGetLocDevMap, count, func(i int) (codec.PacketType, []byte) {
		if i == odd {
			return codec.MsgAck, nil
		}
		if bad {
			return codec.MsgRetDevInfo, []byte{0x01}
		}
		info := codec.DeviceInfo{
			ProtocolVersion: codec.ProtocolVersion,
			Address:         gatewayAddr,
			ID:              codec.ID{TypeID: 636, Name: "Centra"},
			Status:          codec.UnitStatus{Status: codec.StatusPresent},
		}
		payload, _ := info.AppendTo(nil)
		return codec.MsgRetDevInfo, payload
	})
}

// block answers a multi-packet transfer: a header, then one item per fetch.
type blockState struct {
	count int
	item  func(int) (codec.PacketType, []byte)
}

func (d *device) block(f codec.Frame, req codec.PacketType, count int, item func(int) (codec.PacketType, []byte)) {
	d.mu.Lock()
	if d.blocks == nil {
		d.blocks = make(map[int16]*blockState)
	}
	d.blocks[f.Src.Index] = &blockState{count: count, item: item}
	d.mu.Unlock()

	hdr := codec.BlockHeader{PktType: req, Count: uint16(count), MaxSize: codec.MaxPayload}
	d.reply(f, codec.MsgBlockHeader, hdr.AppendTo(nil))
}

func (d *device) menuNext(f codec.Frame) {
	next, err := codec.DecodeGetNext(f.Payload)
	if err != nil {
		d.reply(f, codec.MsgNack, nil)
		return
	}

	d.mu.Lock()
	st := d.blocks[f.Src.Index]
	d.mu.Unlock()

	if st == nil || int(next.Index) >= st.count {
		d.reply(f, codec.MsgNack, nil)
		return
	}
	typ, payload := st.item(int(next.Index))
	d.reply(f, typ, payload)
}

// menuBlock answers the 16-bit menu request.
func (d *device) menuBlock(f codec.Frame) {
	port := d.port(f)

	d.mu.Lock()
	items := d.menus[port]
	odd := d.oddMenuItem
	d.mu.Unlock()

	d.block(f, codec.MsgGetFunc, len(items), func(i int) (codec.PacketType, []byte) {
		if i == odd {
			// Not a menu line at all. A walker should skip it and keep the
			// rest of the menu.
			return codec.MsgAck, nil
		}
		fn, err := items[i].ToFunc()
		if err != nil {
			// A menu that cannot be narrowed is a menu this device should
			// not have been asked for in this generation.
			d.fail("device: line %d does not fit the 16-bit generation: %v", i, err)
			return codec.MsgNack, nil
		}
		payload, _ := fn.AppendTo(nil)
		return codec.MsgRetFunc, payload
	})
}

func (d *device) menuCount(f codec.Frame) {
	port := d.port(f)

	d.mu.Lock()
	items := d.menus[port]
	d.mu.Unlock()

	req, err := codec.DecodeMenuReq(f.Payload)
	if err != nil {
		d.reply(f, codec.MsgNack, nil)
		return
	}
	size := codec.MenuSize{MenuIndex: req.MenuIndex, MenuCount: uint32(len(items))}
	d.reply(f, codec.MsgRetMenuCount, size.AppendTo(nil))
}

func (d *device) menuItem(f codec.Frame) {
	port := d.port(f)

	req, err := codec.DecodeMenuReq(f.Payload)
	if err != nil {
		d.reply(f, codec.MsgNack, nil)
		return
	}

	d.mu.Lock()
	items := d.menus[port]
	d.mu.Unlock()

	if int(req.MenuIndex) >= len(items) {
		d.reply(f, codec.MsgNack, nil)
		return
	}
	payload, err := items[req.MenuIndex].AppendTo(nil)
	if err != nil {
		d.fail("device: encode menu item: %v", err)
		return
	}
	d.reply(f, codec.MsgRetMenuItem, payload)
}

func (d *device) getValue(f codec.Frame) {
	port := d.port(f)

	req, err := codec.DecodeGetValue(f.Payload)
	if err != nil {
		d.reply(f, codec.MsgNack, nil)
		return
	}

	d.mu.Lock()
	r := d.routers[port]
	d.mu.Unlock()

	if r != nil {
		if r.garbled(req.Command) {
			// A reply too short for the structure it claims to be.
			d.reply(f, codec.MsgRetValue, []byte{0x01})
			return
		}
		v, mine, ok := r.get(req.Command)
		if !mine || !ok {
			// A command outside the routing interface is refused, which is how
			// a client tells a router node from anything else.
			d.reply(f, codec.MsgNack, nil)
			return
		}
		payload, err := v.AppendTo(nil)
		if err != nil {
			d.fail("device: encode router value: %v", err)
			return
		}
		d.reply(f, codec.MsgRetValue, payload)
		return
	}

	d.mu.Lock()
	v, ok := d.values[port][req.Command]
	d.mu.Unlock()

	if !ok {
		d.reply(f, codec.MsgNack, nil)
		return
	}
	payload, err := v.AppendTo(nil)
	if err != nil {
		d.fail("device: encode value: %v", err)
		return
	}
	d.reply(f, codec.MsgRetValue, payload)
}

// setValueMsg writes a value and answers with what the device stored, which is
// how a caller learns that a device clamped what it asked for.
func (d *device) setValueMsg(f codec.Frame) {
	port := d.port(f)

	v, err := codec.DecodeValue(f.Payload)
	if err != nil {
		d.reply(f, codec.MsgNack, nil)
		return
	}

	d.mu.Lock()
	r := d.routers[port]
	d.mu.Unlock()

	if r != nil {
		if r.garbled(v.Command) {
			d.reply(f, codec.MsgRetValue, []byte{0x01})
			return
		}
		reply, pushed, mine := r.set(v)
		if !mine {
			d.reply(f, codec.MsgNack, nil)
			return
		}
		payload, err := reply.AppendTo(nil)
		if err != nil {
			d.fail("device: encode router reply: %v", err)
			return
		}
		d.reply(f, codec.MsgRetValue, payload)

		// The new value goes out afterwards, on the back channel, which is the
		// only place a client ever sees it.
		if pushed != nil {
			body, err := pushed.AppendTo(nil)
			if err != nil {
				d.fail("device: encode router push: %v", err)
				return
			}
			d.push(port, codec.MsgRetValue, body)
		}
		return
	}

	stored := d.store(port, v)
	payload, err := stored.AppendTo(nil)
	if err != nil {
		d.fail("device: encode value: %v", err)
		return
	}
	d.reply(f, codec.MsgRetValue, payload)
}

// store applies a write, clamping to the menu's range the way a device does.
func (d *device) store(port uint8, v codec.Value) codec.Value {
	d.mu.Lock()
	defer d.mu.Unlock()

	if v.Mode.Has(codec.ModePreset) {
		// A preset write asks for the device's own default, and the numeric
		// field is ignored. The default here is the bottom of the range.
		for _, m := range d.menus[port] {
			if m.Command == v.Command {
				v = codec.Value{Command: v.Command, Mode: codec.ModeValue, Val: m.MinRange}
			}
		}
	} else {
		for _, m := range d.menus[port] {
			if m.Command != v.Command || !v.Mode.Has(codec.ModeValue) {
				continue
			}
			// Only a numeric line has a range to clamp against. On a
			// checkbox the minimum field is the "on" value and on a button it
			// is the value the press writes, so clamping either would turn a
			// legitimate write into its opposite.
			switch m.Style.Kind() {
			case codec.StyleNumber, codec.StyleVGraph, codec.StyleHGraph,
				codec.StyleVLevel, codec.StyleHLevel:
				if v.Val < m.MinRange {
					v.Val = m.MinRange
				}
				if v.Val > m.MaxRange {
					v.Val = m.MaxRange
				}
			}
		}
	}
	if d.values[port] == nil {
		d.values[port] = make(map[uint32]codec.Value)
	}
	d.values[port][v.Command] = v
	return v
}

func (d *device) getFStat(f codec.Frame) {
	port := d.port(f)

	req, err := codec.DecodeGetFStat(f.Payload)
	if err != nil {
		d.reply(f, codec.MsgNack, nil)
		return
	}

	d.mu.Lock()
	v, ok := d.values[port][uint32(req.Command)]
	d.mu.Unlock()

	if !ok {
		d.reply(f, codec.MsgNack, nil)
		return
	}
	fs, err := v.ToFuncStatus()
	if err != nil {
		d.reply(f, codec.MsgNack, nil)
		return
	}
	payload, err := fs.AppendTo(nil)
	if err != nil {
		d.fail("device: encode func status: %v", err)
		return
	}
	d.reply(f, codec.MsgRetFStat, payload)
}

func (d *device) setParam(f codec.Frame) {
	port := d.port(f)

	fs, err := codec.DecodeFuncStatus(f.Payload)
	if err != nil {
		d.reply(f, codec.MsgNack, nil)
		return
	}

	stored := d.store(port, fs.ToValue())
	back, err := stored.ToFuncStatus()
	if err != nil {
		d.reply(f, codec.MsgNack, nil)
		return
	}
	payload, err := back.AppendTo(nil)
	if err != nil {
		d.fail("device: encode func status: %v", err)
		return
	}
	d.reply(f, codec.MsgRetFStat, payload)
}

func (d *device) dispData(f codec.Frame) {
	if len(f.Payload) < 2 {
		d.reply(f, codec.MsgNack, nil)
		return
	}
	line := int16(uint16(f.Payload[0])<<8 | uint16(f.Payload[1]))
	if line > 1 {
		// This device only has two status lines, which is ordinary.
		d.reply(f, codec.MsgNack, nil)
		return
	}
	disp := codec.Disp{Line: line, Text: "line " + string(rune('0'+line))}
	payload, err := disp.AppendTo(nil)
	if err != nil {
		d.fail("device: encode display: %v", err)
		return
	}
	d.reply(f, codec.MsgDispData, payload)
}

func (d *device) backChannelReady(f codec.Frame) {
	if len(f.Payload) != 1 {
		d.reply(f, codec.MsgNack, nil)
		return
	}
	d.mu.Lock()
	d.backChannel[f.Src.Index] = f.Payload[0] != codec.BackChannelDisable
	d.mu.Unlock()
	d.reply(f, codec.MsgAck, nil)
}

func (d *device) fileOpen(f codec.Frame) {
	req, path, err := codec.DecodeFileOpen(f.Payload)
	if err != nil {
		d.reply(f, codec.MsgNack, nil)
		return
	}

	// A device serves binary files, and a client that forgot the flag would
	// get line endings translated. Catching it here is what makes the
	// consumer's insistence on the flag testable.
	if req.OpenFlags()&codec.OpenBinary == 0 {
		d.fail("device: %q was opened without the binary flag (%04X)", path, req.OpenFlags())
	}

	d.mu.Lock()
	body, ok := d.files[path]
	size := d.blockSize
	d.mu.Unlock()

	if !ok {
		fail := codec.File{SrcHandle: req.SrcHandle, Extra: codec.FileErrNoEntry}
		d.reply(f, codec.MsgRetFileOpen, fail.AppendTo(nil))
		return
	}

	d.mu.Lock()
	if d.openFiles == nil {
		d.openFiles = make(map[int16][]byte)
	}
	handle := int16(len(d.openFiles) + 1)
	d.openFiles[handle] = body
	d.mu.Unlock()

	// The reply overwrites both numeric fields: the offset becomes the
	// device's own maximum read size, and the extra becomes the error.
	ok2 := codec.File{SrcHandle: req.SrcHandle, FileHandle: handle, Offset: size}
	d.reply(f, codec.MsgRetFileOpen, ok2.AppendTo(nil))
}

// fileDir answers a directory listing as a multi-packet transfer.
func (d *device) fileDir(f codec.Frame) {
	d.mu.Lock()
	names := make([]string, 0, len(d.files))
	for name := range d.files {
		names = append(names, name)
	}
	sizes := make(map[string]int, len(d.files))
	for name, body := range d.files {
		sizes[name] = len(body)
	}
	garble := d.garbleDirEntry
	odd := d.oddDirItem
	d.mu.Unlock()
	sort.Strings(names)

	d.block(f, codec.MsgFileDir, len(names), func(i int) (codec.PacketType, []byte) {
		if i == odd {
			return codec.MsgAck, nil
		}
		if garble {
			return codec.MsgRetFileDir, []byte{0x01}
		}
		entry := codec.DirEntry{
			Info: codec.FileInfo{Length: int32(sizes[names[i]])},
			Name: names[i],
		}
		payload, err := entry.AppendTo(nil)
		if err != nil {
			d.fail("device: encode directory entry: %v", err)
			return codec.MsgNack, nil
		}
		return codec.MsgRetFileDir, payload
	})
}

func (d *device) fileRead(f codec.Frame) {
	req, err := codec.DecodeFile(f.Payload)
	if err != nil {
		d.reply(f, codec.MsgNack, nil)
		return
	}

	d.mu.Lock()
	body, ok := d.openFiles[req.FileHandle]
	failAfter := d.failReadAfter
	short := d.shortReadCount
	endless := d.endlessFile
	if failAfter >= 0 {
		d.failReadAfter--
	}
	d.mu.Unlock()

	if failAfter == 0 {
		// The device gives up part way through, which is what a card being
		// pulled during a template download looks like.
		fail := codec.File{SrcHandle: req.SrcHandle, Extra: codec.FileErrAccess}
		d.reply(f, codec.MsgRetFileRead, fail.AppendTo(nil))
		return
	}

	if !ok {
		fail := codec.File{SrcHandle: req.SrcHandle, Extra: codec.FileErrInvalid}
		d.reply(f, codec.MsgRetFileRead, fail.AppendTo(nil))
		return
	}

	// On a read the offset says where and the extra says how many. A client
	// that sends a zero count would read nothing for ever, so the device
	// says so rather than looping.
	count := int(req.Extra)
	if count <= 0 {
		d.fail("device: a read asked for %d bytes; the count belongs in the extra field", count)
		count = 0
	}

	off := int(req.Offset)
	if off > len(body) {
		off = len(body)
	}
	end := off + count
	if end > len(body) {
		end = len(body)
	}
	chunk := body[off:end]

	if endless {
		// Never reaches the end, so a client with no ceiling would read for
		// ever.
		chunk = body
		if len(chunk) == 0 {
			chunk = []byte{0}
		}
	}

	// In the reply the offset is how many bytes were read and the extra is
	// the error, which is the opposite of the request.
	count = len(chunk)
	if short && count > 1 {
		// The reply understates what it carries. The count is what a client
		// must believe: reading past it would take bytes the device did not
		// mean to send.
		count--
	}
	hdr := codec.File{SrcHandle: req.SrcHandle, FileHandle: req.FileHandle, Offset: int32(count)}
	d.reply(f, codec.MsgRetFileRead, append(hdr.AppendTo(nil), chunk...))
}
