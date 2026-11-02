package cerebrumnb

import "dhs/internal/consumer"

// WriteGuardReason declares that every write through this plugin must be
// confirmed with --write-to HOST (consumer.WriteGuard, ADR-0037 P5): the
// generic set, import and ensure refuse to write without it and stay with
// the dry run.
func (p *Plugin) WriteGuardReason() string { return consumer.WriteGuardEveryConnector }
