package probelsw02p

import "acp/internal/probel-sw02p/codec"

// DefaultPort is the TCP port this plugin binds when the caller does
// not supply one. Mirror of codec.DefaultPort so cmd/ code can import
// this package alone without pulling in the byte-codec package directly.
const DefaultPort = codec.DefaultPort
