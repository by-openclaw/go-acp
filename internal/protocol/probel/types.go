package probel

import (
	iprobel "acp/internal/probel"
)

// DefaultPort is the TCP port most Probel matrices expose for SW-P-08.
// Mirror of internal/probel.DefaultPort so cmd/ code can import this
// package alone without pulling in the byte-codec package directly.
const DefaultPort = iprobel.DefaultPort
