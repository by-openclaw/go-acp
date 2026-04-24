package codec

// ProtectState is the 2-bit protect-status field carried by the
// Extended PROTECT family (tx 96 / 97 / 98) and by the per-entry
// Device Number & Protect Data word in tx 100 Extended PROTECT TALLY
// DUMP. See §3.2.60 for the canonical table.
//
//	| Value | Meaning                                                  |
//	|-------|----------------------------------------------------------|
//	|   0   | Not Protected                                            |
//	|   1   | Pro-Bel Protected                                        |
//	|   2   | Pro-Bel override Protected (cannot be altered remotely)  |
//	|   3   | OEM / Router Protected                                   |
type ProtectState uint8

// Protect state values — §3.2.60.
const (
	ProtectNone          ProtectState = 0
	ProtectProBel        ProtectState = 1
	ProtectProBelOverride ProtectState = 2
	ProtectOEM           ProtectState = 3
)
