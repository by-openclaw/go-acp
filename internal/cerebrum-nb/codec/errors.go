package codec

import "fmt"

// NackCode is the §6 error-code enum carried in <nack>'s `id` /
// `code` attributes. ID values are stable; code strings are the
// canonical spelling (note the British "Licence" in code 12).
type NackCode int

const (
	NackInvalidUserOrPass         NackCode = 0
	NackMtidError                 NackCode = 1
	NackUnknownCommand            NackCode = 2
	NackInvalidXML                NackCode = 3
	NackServerInactive            NackCode = 4
	NackUnknownConnection         NackCode = 5
	NackNotLoggedIn               NackCode = 6
	NackCommandMissingParameters  NackCode = 7
	NackOneOrMoreActionsInvalid   NackCode = 8
	NackOneOrMoreEventsInvalid    NackCode = 9
	NackOneOrMoreObtainsInvalid   NackCode = 10
	NackResponseTooLarge          NackCode = 11
	NackNoLicenceAvailable        NackCode = 12
	NackOK                        NackCode = 13
)

// nackTable maps id ↔ canonical code string ↔ description, exactly per
// keys.md §6.
var nackTable = []struct {
	ID    NackCode
	Code  string
	Desc  string
}{
	{NackInvalidUserOrPass, "INVALID_USER_OR_PASS", "specified username or password is invalid"},
	{NackMtidError, "MTID_ERROR", "a message type identifier was not specified"},
	{NackUnknownCommand, "UNKNOWN_COMMAND", "an unknown command has been specified"},
	{NackInvalidXML, "INVALID_XML", "the message XML from a client cannot be loaded"},
	{NackServerInactive, "SERVER_INACTIVE", "the connected server is inactive"},
	{NackUnknownConnection, "UNKNOWN_CONNECTION", "the connection is unrecognized — a new one must be established"},
	{NackNotLoggedIn, "NOT_LOGGED_IN", "a successful login is required"},
	{NackCommandMissingParameters, "COMMAND_MISSING_PARAMETERS", "both a username and password must be specified"},
	{NackOneOrMoreActionsInvalid, "ONE_OR_MORE_ACTIONS_INVALID", "the specified action failed to complete"},
	{NackOneOrMoreEventsInvalid, "ONE_OR_MORE_EVENTS_INVALID", "the specified event subscription failed to complete"},
	{NackOneOrMoreObtainsInvalid, "ONE_OR_MORE_OBTAINS_INVALID", "the specified obtain failed to complete"},
	{NackResponseTooLarge, "RESPONSE_TOO_LARGE", "the XML message is too large to be sent to a client"},
	{NackNoLicenceAvailable, "NO_LICENCE_AVAILABLE", "the Cerebrum server has no licences available"},
	{NackOK, "OK", "the request completed successfully"},
}

// Code returns the canonical UPPER_SNAKE code string.
func (c NackCode) Code() string {
	if int(c) < 0 || int(c) >= len(nackTable) {
		return "UNKNOWN"
	}
	return nackTable[c].Code
}

// Description returns the §6 human-readable description.
func (c NackCode) Description() string {
	if int(c) < 0 || int(c) >= len(nackTable) {
		return ""
	}
	return nackTable[c].Desc
}

// String returns "ID:CODE — desc" for log lines.
func (c NackCode) String() string {
	if int(c) < 0 || int(c) >= len(nackTable) {
		return fmt.Sprintf("%d:UNKNOWN", int(c))
	}
	return fmt.Sprintf("%d:%s — %s", int(c), nackTable[c].Code, nackTable[c].Desc)
}

// NackCodeFromString resolves a canonical code string to its NackCode.
// Lookup is case-insensitive on the string.
func NackCodeFromString(s string) (NackCode, bool) {
	for _, e := range nackTable {
		if strEqualFold(e.Code, s) {
			return e.ID, true
		}
	}
	return -1, false
}

// NackError is the typed error a Decode-side caller bubbles up when
// the wire carried a <nack>.
type NackError struct {
	MTID    string
	ID      NackCode
	Code    string // raw on-wire value when ID didn't resolve
	Message string
}

func (e *NackError) Error() string {
	if e.ID >= 0 {
		return fmt.Sprintf("cerebrum-nb: nack %s", e.ID)
	}
	return fmt.Sprintf("cerebrum-nb: nack %s (id?) — %s", e.Code, e.Message)
}

// strEqualFold is a tiny case-insensitive ASCII equality helper. Avoids
// pulling strings.EqualFold's full unicode machinery; codes are ASCII.
func strEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 32
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
