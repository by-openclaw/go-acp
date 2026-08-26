// Layer-3 -- watching for an IS-09 System API, not just looking once.
//
// A one-shot fetch at startup is the obvious implementation and it is
// wrong for the same reason a one-shot Registry lookup would be: the
// System API is discovered by mDNS, and mDNS is a running conversation
// rather than a question with an answer. The System API a Node needs
// may be advertised after the Node boots -- during a maintenance
// window, after the config server is restarted, or simply because the
// Node came up first -- and a Node that looked once never learns.
//
// IS-09 §4 is explicit that the Node re-resolves when the
// advertisement changes, and the AMWA IS-09-02 suite checks exactly
// that: it advertises a System API mid-run and waits for the Node to
// come and read it.
//
// Priority is re-evaluated on every change, because that is the point
// of `pri`: an operator who advertises a better System API expects
// devices to move to it without being restarted.

package provider

import (
	"context"
	"log/slog"
	"sync"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	dnssdsession "dhs/internal/amwa/session/dnssd"
	systemsession "dhs/internal/amwa/session/system"
)

// SystemWatcher browses `_nmos-system._tcp` and fetches the global
// resource whenever the best advertised instance changes.
type SystemWatcher struct {
	logger *slog.Logger
	apiVer string
	// onGlobal receives every successfully fetched global resource.
	onGlobal func(g any, url string)

	browser dnssdsession.Browser
	cancel  context.CancelFunc

	mu sync.Mutex
	// seen is every currently-advertised instance, keyed by its full
	// DNS-SD name. Kept whole rather than reduced to "the best so far"
	// because an instance going away can promote a different one, and
	// a running best-so-far cannot be un-picked.
	seen map[string]dnssdcodec.Instance
	// fetched is the instance we last read a global from, so a repeat
	// advertisement of the same thing does not re-fetch on every
	// mDNS refresh.
	fetched string
}

// NewSystemWatcher opens the browser. It does not start browsing.
func NewSystemWatcher(logger *slog.Logger, apiVer string, onGlobal func(g any, url string)) (*SystemWatcher, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if apiVer == "" {
		apiVer = "v1.0"
	}
	br, err := dnssdsession.NewBrowser(logger)
	if err != nil {
		return nil, err
	}
	return &SystemWatcher{
		logger:   logger,
		apiVer:   apiVer,
		onGlobal: onGlobal,
		browser:  br,
		seen:     map[string]dnssdcodec.Instance{},
	}, nil
}

// Run starts the browse loop and returns immediately.
func (w *SystemWatcher) Run(ctx context.Context) error {
	loopCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	out, err := w.browser.Browse(loopCtx, dnssdcodec.ServiceSystem)
	if err != nil {
		cancel()
		return err
	}
	go w.consume(loopCtx, out)
	return nil
}

// Close stops the browse loop.
func (w *SystemWatcher) Close() error {
	if w.cancel != nil {
		w.cancel()
	}
	return w.browser.Close()
}

func (w *SystemWatcher) consume(ctx context.Context, out <-chan dnssdcodec.Instance) {
	for {
		select {
		case <-ctx.Done():
			return
		case ins, ok := <-out:
			if !ok {
				return
			}
			w.observe(ctx, ins)
		}
	}
}

// observe records one advertisement and re-picks the best instance.
func (w *SystemWatcher) observe(ctx context.Context, ins dnssdcodec.Instance) {
	key := ins.Name + "." + ins.Service
	w.mu.Lock()
	if ins.TTL == 0 {
		// A goodbye packet. Removing it can PROMOTE another instance,
		// which is why the whole set is kept rather than a running
		// best.
		delete(w.seen, key)
		if w.fetched == key {
			w.fetched = ""
		}
	} else {
		w.seen[key] = ins
	}
	candidates := make([]dnssdcodec.Instance, 0, len(w.seen))
	for _, v := range w.seen {
		candidates = append(candidates, v)
	}
	already := w.fetched
	w.mu.Unlock()

	if len(candidates) == 0 {
		return
	}
	best, err := systemsession.SelectInstance(candidates, "http", w.apiVer, w.logger)
	if err != nil {
		return
	}
	bestKey := best.Name + "." + best.Service
	if bestKey == already {
		return
	}

	res, err := systemsession.Fetch(ctx, systemsession.IS09FetchOptions{
		Logger:     w.logger,
		APIVer:     w.apiVer,
		Discovered: []dnssdcodec.Instance{best},
	})
	if err != nil || res == nil || res.Global == nil {
		w.logger.Warn("provider/node: System API advertised but /global could not be read",
			"plugin", "amwa", "api", "is-09", "instance", best.Name, "err", err)
		return
	}
	w.mu.Lock()
	w.fetched = bestKey
	w.mu.Unlock()
	if w.onGlobal != nil {
		w.onGlobal(res.Global, res.URL)
	}
}
