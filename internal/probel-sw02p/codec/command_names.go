package codec

// CommandName returns a human-readable label for a SW-P-02 command
// byte. Empty string when the id is not known to this codec. Used by
// metrics reporting (top-commands tables, Prom label sets, CSV/MD
// exports) and by log lines that want a symbolic name instead of raw
// hex.
//
// Source of truth: §3 of SW-P-02 Issue 26. The catalogue is populated
// per-command in follow-up commits; the scaffold defines none.
func CommandName(id CommandID) string {
	_ = id
	return ""
}

// CommandIDs returns every SW-P-02 command byte this codec knows. The
// order is not guaranteed; callers that need to iterate for
// registration should treat the result as a set. Empty until per-
// command files land.
func CommandIDs() []CommandID { return nil }
