package emberplus

// autoSubscribeStreams enumerates every already-walked stream-backed
// Parameter and sends Command 30 (Subscribe, spec p.30–31) for each,
// recording it in streamSubs so unsubscribeAll / Disconnect release
// the provider-side subscription on teardown.
//
// Called from Subscribe() when the caller registers the wildcard "*"
// callback. Newly discovered stream Parameters (arriving during a
// subsequent walk or announce) are handled from processParameter via
// maybeWildcardStreamSubscribe — this function only covers what is
// already in the tree at the moment wildcard subscribe happens.
//
// Idempotent: a stream already in streamSubs is skipped.
func (p *Plugin) autoSubscribeStreams() {
	s := p.currentSession()
	if s == nil {
		return
	}
	p.treeMu.RLock()
	type streamTarget struct {
		path []int32
		id   int64
	}
	targets := make([]streamTarget, 0)
	for _, e := range p.numIndex {
		if e.glowParam == nil || e.glowParam.StreamIdentifier == 0 {
			continue
		}
		// Use the entry's resolved numericPath, NOT glowParam.Path.
		// Non-qualified providers (smh, DHD) omit Path on the wire;
		// we compute the canonical numeric path from parent context
		// during decode. glowParam.Path would be empty here.
		if len(e.numericPath) == 0 {
			continue
		}
		targets = append(targets, streamTarget{
			path: cloneInt32Slice(e.numericPath),
			id:   e.glowParam.StreamIdentifier,
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
			p.logger.Debug("emberplus: wildcard stream auto-subscribe failed",
				"path", key, "err", err)
			continue
		}
		p.logger.Debug("emberplus: wildcard stream auto-subscribe",
			"path", key, "stream_identifier", t.id)
	}
}

// maybeWildcardStreamSubscribe auto-subscribes a stream-backed
// Parameter the plugin just stored if wildcard watch is active and
// this stream has not been subscribed yet. Called from
// processParameter after the entry is stored — that is when we first
// learn the parameter has a non-zero StreamIdentifier.
func (p *Plugin) maybeWildcardStreamSubscribe(entry *treeEntry) {
	if entry == nil || entry.glowParam == nil {
		return
	}
	if entry.glowParam.StreamIdentifier == 0 {
		return
	}
	// Use entry.numericPath (canonical numeric RelOID, resolved
	// from parent context for non-qualified providers). glowParam.Path
	// is empty on non-qualified wire frames — smh / DHD providers
	// send nearly everything non-qualified.
	if len(entry.numericPath) == 0 {
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
		p.logger.Debug("emberplus: wildcard stream subscribe (discovery) failed",
			"path", key, "err", err)
		return
	}
	p.logger.Debug("emberplus: wildcard stream subscribe (discovery)",
		"path", key, "stream_identifier", entry.glowParam.StreamIdentifier)
}
