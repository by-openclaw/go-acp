package codec

import (
	"encoding/json"
	"errors"
	"strings"
)

// Message is the body of consumer/postMessage (§5.39.2): a row added
// to the MsgExchange table, which the core dispatches.
//
//	{"MCII":1,"NumArgs":2,"Message":"cpiasd/move/command/targetAz,120000"}
//
// RPID names the consumer inside the core; omitted it defaults to 4,
// the SVTS Manager, which forwards the message (§5.39.2).
type Message struct {
	MCII    int    `json:"MCII"`
	NumArgs int    `json:"NumArgs"`
	Message string `json:"Message"`
	RPID    int    `json:"RPID,omitempty"`
}

// MCII values the document gives a client. 1 is a SET (§5.39.2:
// "MCII should be 1 from a client for a SET message"); 2 is the value
// of the only command-verb example (§9). The document does not define
// MCII further.
const (
	MCIISet     = 1
	MCIICommand = 2
)

// Errors from the message builders: the document does not validate
// arguments ("misformatted commands … are not rejected visibly", §9),
// so the client refuses what it can see is malformed.
var (
	ErrEmptyUID       = errors.New("kratos: empty UID")
	ErrCommaInUID     = errors.New("kratos: a UID cannot contain a comma (the SET message separates UID and value with one)")
	ErrEmptyVerb      = errors.New("kratos: empty command verb")
	ErrSpaceInVerb    = errors.New("kratos: a command verb is one word (arguments follow after a space)")
	ErrSpaceInCommand = errors.New("kratos: a command argument cannot contain a space (arguments are space-separated)")
)

// SetMessage builds the SET of one value (§5.39.2): MCII 1, NumArgs 2,
// Message "<UID>,<value>". The value is sent as given; scaling is the
// caller's (the SVT unitScale column describes it).
func SetMessage(uid, value string) (Message, error) {
	switch {
	case strings.TrimSpace(uid) == "":
		return Message{}, ErrEmptyUID
	case strings.Contains(uid, ","):
		return Message{}, ErrCommaInUID
	}
	return Message{MCII: MCIISet, NumArgs: 2, Message: uid + "," + value}, nil
}

// CommandMessage builds a command verb (§9): MCII 2, NumArgs 1, the
// verb and its arguments in one Message string separated by spaces —
// the form of the only example ("RECOMPUTE-SAT-FAVORITE-LIST"). The
// document gives no NumArgs for a verb with arguments; this sends 1
// (one Message string) and the arguments inside it, as §9 describes
// them ("provided in the string separated by spaces").
func CommandMessage(verb string, args ...string) (Message, error) {
	verb = strings.TrimSpace(verb)
	switch {
	case verb == "":
		return Message{}, ErrEmptyVerb
	case strings.ContainsAny(verb, " \t"):
		return Message{}, ErrSpaceInVerb
	}
	parts := []string{verb}
	for _, a := range args {
		if strings.ContainsAny(a, " \t") {
			return Message{}, ErrSpaceInCommand
		}
		if a != "" {
			parts = append(parts, a)
		}
	}
	return Message{MCII: MCIICommand, NumArgs: 1, Message: strings.Join(parts, " ")}, nil
}

// Encode returns the JSON body.
func (m Message) Encode() []byte {
	b, _ := json.Marshal(m)
	return b
}

// CommandVerbs is the preliminary list of §9 with the argument each
// takes, "" when none. A verb outside the list is still sent (the list
// is "preliminary"); the CLI says so.
var CommandVerbs = map[string]string{
	"ABANDON":                     "",
	"ACQUIRE":                     "",
	"CLEAR-SMART":                 "",
	"DO-MPAMP-CAL":                "",
	"GOTO-LONGITUDE":              "",
	"INJECT-LOG":                  "text",
	"INCLIN-AQSN":                 "inclination in degrees (optional)",
	"MARK-POS-LOG":                "",
	"PAUSE-NOW":                   "",
	"PEAK-NOW":                    "",
	"POINT-AT-GLOBAL-SAT":         "satellite id",
	"RECOMPUTE-GLOBAL-SAT":        "satellite id",
	"RECOMPUTE-SAT-FAVORITE-LIST": "",
	"RETUNE-GLOBAL-SAT":           "satellite id",
	"SAT-RELOAD":                  "",
	"STANDBY":                     "",
	"SPIRAL-SCAN":                 "",
	"STOW":                        "",
	"SYNC":                        "",
	"RESUME-TRACK":                "",
}
