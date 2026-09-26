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
//     four fields, whoever supplies them — including `dhs host info`, which
//     asks it about the machine dhs runs on (#1147).
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
// # Local capture, per OS
//
// Reading Ethertype 0x88CC means a raw link-layer socket, which every OS
// offers differently:
//
//	Linux    AF_PACKET, stdlib syscall, needs CAP_NET_RAW
//	macOS    /dev/bpf*, stdlib syscall, needs root or access to /dev/bpf*
//	Windows  Npcap — Windows raw sockets are IP-level and never see a
//	         non-IP Ethertype. wpcap.dll is loaded at run time with the
//	         standard library: no CGo, nothing linked into the binary. The
//	         operator installs Npcap (its licence forbids us shipping it).
//
// A host that cannot capture — Windows without Npcap, any other OS — returns
// [ErrCaptureUnsupported] rather than pretending there are no neighbours.
// The host-side posture is recorded in docs/adr/0005-deps.json under
// host_deps and applied by the dhs_capture Ansible role.
package lldp
