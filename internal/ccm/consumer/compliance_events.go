package consumer

// Deviations from CCM 0v1 this connector absorbs and counts. Each is a
// label on the session's compliance profile: the device keeps working
// and every occurrence is auditable.
const (
	// SelfUnderApp: /self carries the product under an `app` object.
	// §15 puts productName and productVersion at the top level.
	SelfUnderApp = "ccm_self_under_app"
	// NoAPIRoot: the API base answers no document, so the tree cannot
	// be walked from it and is built from the api.yml's declared paths
	// (§5.1 has `GET /api` return the domains).
	NoAPIRoot = "ccm_no_api_root"

	// NoWebSocket: the device refused the upgrade. §13.1 has every
	// non-trivial device serve the channel.
	NoWebSocket = "ccm_no_websocket"
	// EventTypeSingular: a notification typed "Event" where §13.3.6 and
	// §13.4 say "Events".
	EventTypeSingular = "ccm_ws_event_type"
	// FrameUnreadable: a frame that is not a §13 message.
	FrameUnreadable = "ccm_ws_frame_unreadable"
	// SubscriptionRefused: the device refused a path its own api.yml
	// declares as a GET (§13.3: every GETtable resource is subscribable).
	SubscriptionRefused = "ccm_ws_subscription_refused"
	// PatchUnapplied: a patch that does not fit the state the device
	// sent before it. The session ends and is rebuilt (§13.4.1).
	PatchUnapplied = "ccm_ws_patch_unapplied"
)
