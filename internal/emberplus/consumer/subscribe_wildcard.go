package emberplus

// subscribeAllParameters enumerates every already-walked Parameter and
// sends Command 30 (Subscribe, spec p.30–31) for each, recording it in
// streamSubs so unsubscribeAll / Disconnect release the provider-side
// subscription on teardown.
//
// Why every Parameter (stream and plain): spec p.31 says Subscribe is
// the consumer's signal to receive value-change announcements from
// that path. Loose providers (TinyEmberPlus, our own provider)
// broadcast to every connected session regardless and treat Subscribe
// as a no-op; strict providers gate announcement emission entirely on
// Subscribe and emit nothing otherwise. To make `dhs consumer
// emberplus watch` work against both kinds of provider we send
// Subscribe per Parameter after the walk completes — loose providers
// ignore it, strict providers start emitting.
//
// Called from Subscribe() when the caller registers the wildcard "*"
// callback. Newly discovered Parameters (arriving during a subsequent
// walk or announce) are handled from processParameter via
// subscribeOnDiscovery — this function only covers what is already in
// the tree at the moment wildcard subscribe happens.
//
// Idempotent: a path already in streamSubs is skipped.
func (p *Plugin) subscribeAllParameters() {
	s := p.currentSession()
	if s == nil {
		return
	}
	p.treeMu.RLock()
	type paramTarget struct {
		path     []int32
		streamID int64
	}
	targets := make([]paramTarget, 0)
	for _, e := range p.numIndex {
		if e.glowParam == nil {
			continue
		}
		// Use the entry's resolved numericPath, NOT glowParam.Path.
		// Non-qualified providers (smh, DHD) omit Path on the wire;
		// we compute the canonical numeric path from parent context
		// during decode. glowParam.Path would be empty here.
		if len(e.numericPath) == 0 {
			continue
		}
		// Respect wildcard filter (--path / --no-streams /
		// --streams-only). Empty filter = subscribe everything.
		if !p.wildcardMatches(e) {
			continue
		}
		targets = append(targets, paramTarget{
			path:     cloneInt32Slice(e.numericPath),
			streamID: e.glowParam.StreamIdentifier,
		})
	}
	p.treeMu.RUnlock()

	for _, t := range targets {
		key := numericKey(t.path)
		p.subsMu.Lock()
		if _, already := p.streamSubs[key]; already {
			p.subsMu.Unlock()
			continue
		}
		p.streamSubs[key] = t.path
		p.subsMu.Unlock()

		if err := s.SendSubscribe(t.path); err != nil {
			p.logger.Debug("emberplus: wildcard subscribe (batch) failed",
				"path", key, "err", err)
			continue
		}
		p.logger.Debug("emberplus: wildcard subscribe (batch)",
			"path", key, "stream_identifier", t.streamID)
	}
}

// subscribeOnDiscovery subscribes to a Parameter the plugin just
// stored, if wildcard watch is active and this path has not been
// subscribed yet. Covers BOTH stream Parameters and plain Parameters
// per spec p.30–31: strict providers only emit value-change
// announcements after an explicit Subscribe(30) regardless of stream
// identifier; loose providers broadcast unconditionally and ignore
// the duplicate subscribe. Called from processParameter after the
// entry is stored.
func (p *Plugin) subscribeOnDiscovery(entry *treeEntry) {
	if entry == nil || entry.glowParam == nil {
		return
	}
	// Use entry.numericPath (canonical numeric RelOID, resolved from
	// parent context for non-qualified providers). glowParam.Path is
	// empty on non-qualified wire frames — smh / DHD providers send
	// nearly everything non-qualified.
	if len(entry.numericPath) == 0 {
		return
	}
	// Respect wildcard filter (--path / --no-streams / --streams-only).
	if !p.wildcardMatches(entry) {
		return
	}

	p.subsMu.RLock()
	_, wildcardActive := p.subs["*"]
	key := numericKey(entry.numericPath)
	_, already := p.streamSubs[key]
	p.subsMu.RUnlock()

	if !wildcardActive || already {
		return
	}

	s := p.currentSession()
	if s == nil {
		return
	}

	pathCopy := cloneInt32Slice(entry.numericPath)
	p.subsMu.Lock()
	p.streamSubs[key] = pathCopy
	p.subsMu.Unlock()

	if err := s.SendSubscribe(pathCopy); err != nil {
		p.logger.Debug("emberplus: wildcard subscribe (discovery) failed",
			"path", key, "err", err)
		return
	}
	p.logger.Debug("emberplus: wildcard subscribe (discovery)",
		"path", key, "stream_identifier", entry.glowParam.StreamIdentifier)
}
