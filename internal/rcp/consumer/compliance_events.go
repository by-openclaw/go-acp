package consumer

// Deviations from the RCP API document (2.6.1) this connector absorbs
// and counts. Each is a label on the session's compliance profile: the
// server keeps working and every occurrence is auditable.
const (
	// SingleIOKey: a single IO answered under the collection's plural
	// name ("sources") where the document says "source" / "destination".
	SingleIOKey = "rcp_single_io_key"
	// ReqIDNotEchoed: an answer whose reqid is missing or is not the one
	// sent. Every schema of the document requires the echo.
	ReqIDNotEchoed = "rcp_reqid_not_echoed"
	// ErrorNotEnveloped: a non-2xx answer that is not the document's
	// {reqid, error{code,message}} object — an HTML page from the HTTP
	// stack in front of the service, for instance.
	ErrorNotEnveloped = "rcp_error_not_enveloped"
)
