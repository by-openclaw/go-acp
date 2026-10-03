//go:build integration

package rollcall_integration

import (
	"time"

	"dhs/internal/export/canonical"
	"dhs/internal/plugin"
	rcconsumer "dhs/internal/snell-rollcall/consumer"
	rcprovider "dhs/internal/snell-rollcall/provider"
)

// loopbackReplyTimeout is the per-message budget every link in this
// suite opens with. The specification's three seconds is what a gateway
// on a wire gets; here both ends are in one process on a CI runner under
// the race detector, where a reply can take longer than that without
// anything being wrong, and a budget that fails the suite then is a
// budget on the runner, not on the protocol (#1199).
const loopbackReplyTimeout = 30 * time.Second

func newConsumer() *rcconsumer.Plugin {
	c := rcconsumer.New(plugin.Deps{}.WithDefaults())
	c.SetReplyTimeout(loopbackReplyTimeout)
	return c
}

func newProvider(tree *canonical.Export) *rcprovider.Provider {
	return newProviderWith(plugin.Deps{}.WithDefaults(), tree)
}

func newProviderWith(deps plugin.Deps, tree *canonical.Export) *rcprovider.Provider {
	p := rcprovider.New(deps, tree)
	p.SetReplyTimeout(loopbackReplyTimeout)
	return p
}
