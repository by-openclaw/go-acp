package emberplus

import "time"

// fastWalk shortens Walk's settle timings for a test.
//
// The production values exist for real devices on real networks: 500 ms for
// the first burst to arrive, then a 2 s quiet period before the tree is
// declared complete, because a DHD console or a 1024×1024 PowerCore matrix
// delivers its contents in trailing frames. Against a loopback provider that
// answers instantly, every one of those seconds is dead time — and seven
// tests were each paying 2.5 s of it.
//
// Tests that are specifically about the settle or grace behaviour set their
// own values and do not use this.
func fastWalk(p *Plugin) *Plugin {
	p.walkInitialDelay = time.Millisecond
	p.walkPollInterval = time.Millisecond
	p.walkSettleInitial = 20 * time.Millisecond
	p.walkSettleInterval = 20 * time.Millisecond
	p.walkGraceInterval = 5 * time.Millisecond
	p.writeTimeout = 50 * time.Millisecond
	return p
}
