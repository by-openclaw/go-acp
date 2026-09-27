//go:build !windows && !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package transport

// soReusePort is 0 where this build knows no SO_REUSEPORT: the port is then
// shared through SO_REUSEADDR alone, as far as the platform allows.
const soReusePort = 0
