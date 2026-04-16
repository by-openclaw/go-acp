package protocol

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry is the process-wide map of protocol name → factory. Plugins
// populate it from their init(); the CLI and API server look up plugins
// here by the --protocol flag value.
//
// Names are normalised to lower-case ASCII. Duplicate registration of the
// same name panics (a programming error — two plugins claiming "acp1").
var (
	regMu sync.RWMutex
	reg   = map[string]ProtocolFactory{}
)

// Register installs a factory under its Meta().Name. Called from plugin
// init(). Panics on duplicate name.
func Register(f ProtocolFactory) {
	if f == nil {
		panic("protocol.Register: nil factory")
	}
	name := strings.ToLower(f.Meta().Name)
	if name == "" {
		panic("protocol.Register: empty name")
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := reg[name]; dup {
		panic(fmt.Sprintf("protocol.Register: duplicate name %q", name))
	}
	reg[name] = f
}

// Get looks up a factory by name (case-insensitive). Returns an error
// rather than nil so callers can surface a useful CLI/API message.
func Get(name string) (ProtocolFactory, error) {
	regMu.RLock()
	defer regMu.RUnlock()
	f, ok := reg[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("protocol %q not registered (available: %s)",
			name, strings.Join(listLocked(), ", "))
	}
	return f, nil
}

// List returns all registered protocol names, alphabetically sorted. Used
// by the CLI's --protocol help text and the API's GET /api/protocols.
func List() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	return listLocked()
}

func listLocked() []string {
	out := make([]string, 0, len(reg))
	for name := range reg {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
