// IS-09 System API client -- re-exported from the session layer.
//
// The implementation moved to internal/amwa/session/system because BOTH
// roles need it and the layering forbids the shorter path. A Node is an
// IS-09 CLIENT: IS-09 §4 has every Node discover a System API at
// startup and take its PTP domain, syslog target, and registry list
// from the global resource. But `provider` may not import `consumer`
// (see internal/amwa/CLAUDE.md), so the choice was to duplicate the
// discovery-and-selection rules or to lift them one layer down. Two
// copies of a selection rule drift, and this one decides which
// Registry a device trusts.
//
// These aliases keep `dhs consumer nmos system` working unchanged.

package consumer

import (
	systemsession "dhs/internal/amwa/session/system"
)

// IS09FetchOptions configures a single Fetch call.
type IS09FetchOptions = systemsession.IS09FetchOptions

// FetchResult bundles the selected instance with the validated Global
// resource.
type FetchResult = systemsession.FetchResult

// ErrNoInstances signals that no usable instance survived selection.
var ErrNoInstances = systemsession.ErrNoInstances

// Fetch picks the highest-priority IS-09 instance and GETs /global.
var Fetch = systemsession.Fetch

// SelectInstance applies the IS-09 §3.1 selection rule.
var SelectInstance = systemsession.SelectInstance

// DiscoverMDNS browses _nmos-system._tcp on the local link.
var DiscoverMDNS = systemsession.DiscoverMDNS

// DiscoverUnicast resolves _nmos-system._tcp via authoritative DNS-SD.
var DiscoverUnicast = systemsession.DiscoverUnicast
