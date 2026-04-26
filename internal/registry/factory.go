package registry

import (
	"fmt"
	"sort"
	"sync"
)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register installs a factory under its Meta().Name. Panics if the
// name is already taken — two plugins cannot share a name.
func Register(f Factory) {
	mu.Lock()
	defer mu.Unlock()
	name := f.Meta().Name
	if _, exists := factories[name]; exists {
		panic(fmt.Sprintf("registry: duplicate registration %q", name))
	}
	factories[name] = f
}

// Lookup returns the factory registered under name.
func Lookup(name string) (Factory, bool) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := factories[name]
	return f, ok
}

// List returns registered names, sorted for deterministic output.
func List() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(factories))
	for n := range factories {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
