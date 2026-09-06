// Package lldp is the Link Layer Discovery Protocol concern: IEEE 802.1AB
// frames, decoded into the neighbour a host sees on one interface.
//
// It exists because two unrelated questions in this repo need the same
// answer shape, and neither is a protocol connector's business:
//
//   - an AMWA NMOS Node must publish interfaces[].attached_network_device,
//     which IS-04 v1.3 defines as "the Chassis ID … as signalled in LLDP
//     received by this Node";
//   - an operator asking which switch port a device is on wants the same
//     four fields, whoever supplies them.
//
// So this package is neutral infrastructure, a sibling of transport and
// auth. It is not owned by any protocol, and no protocol package reaches
// through it into another — internal/amwa/dependencies_test.go fails the
// build on that.
//
// # The split that matters
//
// Decoding LLDP is pure bytes: stdlib, no I/O, every OS, no privileges.
// OBTAINING the bytes is where the platforms diverge sharply, so the two
// are separate. [Source] is the seam:
//
//	Decode      always available, everywhere
//	Source      who supplies neighbours — injected, never assumed
//	Capture     one Source: raw frames off a local interface
//
// A device that reports its own LLDP over an API is as valid a Source as a
// capture, needs no privileges, and is the common case in a plant. Local
// capture is the special case, not the default.
//
// # Why local capture is not portable
//
// Reading Ethertype 0x88CC means a raw link-layer socket:
//
//	Linux    AF_PACKET, stdlib syscall, needs CAP_NET_RAW
//	macOS    /dev/bpf*, stdlib syscall, needs root
//	Windows  impossible from stdlib — Windows raw sockets are IP-level and
//	         never see a non-IP Ethertype. It needs the Npcap driver, whose
//	         free licence permits five systems and no redistribution, and
//	         whose silent installer is OEM-only.
//
// Windows therefore returns [ErrCaptureUnsupported] rather than pretending.
// Nothing here adds a dependency on any OS: the two platforms that can
// capture do it from syscall, and the one that cannot says so in a typed
// error the caller can branch on. The host-side posture is recorded in
// docs/adr/0005-deps.json under host_deps and applied by the dhs_capture
// Ansible role.
package lldp
