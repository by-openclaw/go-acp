package registry

import "os"

// defaultHostname returns os.Hostname; isolated so tests can stub the
// caller in registry.go.
func defaultHostname() (string, error) {
	return os.Hostname()
}
