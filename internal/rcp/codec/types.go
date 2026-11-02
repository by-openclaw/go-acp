// Package codec holds the wire types of the EVS Cerebrum RCP API
// (assets/Cerebrum RCP API-2_6_1.json): JSON bodies over HTTP, every
// answer wrapped in an envelope that echoes the request id.
//
// stdlib-only (ADR-0006): no dhs imports.
package codec

import (
	"encoding/json"
	"fmt"
)

// Collection is one of the four RouteMaster id spaces. Local and
// federation IOs are separate collections with their own ids: the same
// id can exist in two of them and name two different IOs.
type Collection string

const (
	Sources                Collection = "sources"
	Destinations           Collection = "destinations"
	FederationSources      Collection = "federation-sources"
	FederationDestinations Collection = "federation-destinations"
)

// Collections lists the four, in the order the API documents them.
var Collections = []Collection{Sources, Destinations, FederationSources, FederationDestinations}

// ParseCollection accepts a collection as an operator types it.
func ParseCollection(s string) (Collection, error) {
	for _, c := range Collections {
		if string(c) == s {
			return c, nil
		}
	}
	return "", fmt.Errorf("rcp: unknown RouteMaster collection %q (want sources, destinations, federation-sources or federation-destinations)", s)
}

// IsDestination reports whether the collection holds destinations.
func (c Collection) IsDestination() bool {
	return c == Destinations || c == FederationDestinations
}

// IsFederation reports whether the collection is a federation one.
func (c Collection) IsFederation() bool {
	return c == FederationSources || c == FederationDestinations
}

// SingleKey is the envelope key the API document gives a single IO of
// this collection: "source" or "destination".
func (c Collection) SingleKey() string {
	if c.IsDestination() {
		return "destination"
	}
	return "source"
}

// APIError is the error object of a non-2xx answer.
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// APIVersion is the answer of GET /api. The document types the parts as
// strings; Cerebrum 2.5.3 sends numbers and adds a patch version, so
// each part is read as either.
type APIVersion struct {
	Major FlexInt `json:"majorVersion"`
	Minor FlexInt `json:"minorVersion"`
	Patch FlexInt `json:"patchVersion"`
}

func (v APIVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// FlexInt is an integer the peer may send as a JSON number or as a
// string holding one.
type FlexInt int64

func (f *FlexInt) UnmarshalJSON(b []byte) error {
	var n int64
	if err := json.Unmarshal(b, &n); err == nil {
		*f = FlexInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("rcp: %s is neither a number nor a string", b)
	}
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return fmt.Errorf("rcp: %q is not an integer", s)
	}
	*f = FlexInt(n)
	return nil
}

// Login is the answer of POST /login.
type Login struct {
	Token         string `json:"token"`
	WebsocketPort int    `json:"websocketPort"`
}

// Mnemonic is an IO's name and its alternates.
type Mnemonic struct {
	Original   string            `json:"originalMnemonic"`
	Alternates map[string]string `json:"alternateMnemonics,omitempty"`
}

// Device is the binding of an IO on one level, as read.
type Device struct {
	Name           string `json:"name,omitempty"`
	TypeID         int64  `json:"typeId,omitempty"`
	DeviceLevel    int    `json:"deviceLevel,omitempty"`
	IO             int    `json:"io,omitempty"`
	SenderReceiver string `json:"senderReceiver,omitempty"`
	SubChannel     string `json:"subChannel,omitempty"`
	Inhibit        bool   `json:"inhibit,omitempty"`
	Ignore         bool   `json:"ignore,omitempty"`
	Disconnect     bool   `json:"disconnect,omitempty"`
}

// IOLevel is one level an IO exists on, as read.
type IOLevel struct {
	ID            int      `json:"id"`
	Name          string   `json:"name,omitempty"`
	Tags          []string `json:"tags"`
	TagsInherited bool     `json:"tagsInherited"`
	Device        *Device  `json:"device,omitempty"`
}

// IO is one RouteMaster source or destination, local or federation.
// TieLineGroup is carried by destinations only; FederationUID by local
// IOs only (0 = not linked).
type IO struct {
	ID             int64              `json:"id"`
	Mnemonic       Mnemonic           `json:"mnemonic"`
	Virtual        bool               `json:"virtual"`
	FederationUID  int64              `json:"federationUid"`
	TieLineInhibit bool               `json:"tieLineInhibit"`
	TieLineGroup   int                `json:"tieLineGroup"`
	Tags           []string           `json:"tags"`
	Levels         map[string]IOLevel `json:"levels"`
}

// DeviceUpdate is the device binding written on one level. Every field
// is optional; only the supplied ones are modified.
type DeviceUpdate struct {
	Name           *string `json:"name,omitempty"`
	TypeID         *int64  `json:"typeId,omitempty"`
	DeviceLevel    *int    `json:"deviceLevel,omitempty"`
	IO             *int    `json:"io,omitempty"`
	SenderReceiver *string `json:"senderReceiver,omitempty"`
	SubChannel     *string `json:"subChannel,omitempty"`
	Inhibit        *bool   `json:"inhibit,omitempty"`
	Ignore         *bool   `json:"ignore,omitempty"`     // sources only
	Disconnect     *bool   `json:"disconnect,omitempty"` // destinations only
}

// LevelUpdate is what is written on one level of an IO. Tags is a
// pointer so that an empty list (explicitly untagged) is distinct from
// "leave the tags alone".
type LevelUpdate struct {
	Tags        *[]string     `json:"tags,omitempty"`
	TagsInherit *bool         `json:"tagsInherit,omitempty"`
	Device      *DeviceUpdate `json:"device,omitempty"`
	Clear       *bool         `json:"clear,omitempty"`
}

// Update is the body of a PATCH on one IO and, with Count, of a POST
// that creates IOs. Every field is optional; a PATCH needs at least
// one. Levels is keyed "level_<id>".
type Update struct {
	Count          *int                   `json:"count,omitempty"` // create only
	Virtual        *bool                  `json:"virtual,omitempty"`
	Mnemonic       *string                `json:"mnemonic,omitempty"`
	Alternates     map[string]string      `json:"alternateMnemonics,omitempty"`
	TieLineGroup   *int                   `json:"tieLineGroup,omitempty"` // destinations only
	TieLineInhibit *bool                  `json:"tieLineInhibit,omitempty"`
	FederationUID  *int64                 `json:"federationUid,omitempty"` // local IOs only
	Levels         map[string]LevelUpdate `json:"levels,omitempty"`
}

// Empty reports whether the update names no field at all.
func (u Update) Empty() bool {
	return u.Count == nil && u.Virtual == nil && u.Mnemonic == nil && len(u.Alternates) == 0 &&
		u.TieLineGroup == nil && u.TieLineInhibit == nil && u.FederationUID == nil && len(u.Levels) == 0
}

// Validate refuses, before anything is sent, a body the API document
// says the server must refuse.
func (u Update) Validate(c Collection, create bool) error {
	if !create && u.Count != nil {
		return fmt.Errorf("rcp: count is only valid when creating")
	}
	if create && u.Count != nil && *u.Count < 1 {
		return fmt.Errorf("rcp: count must be at least 1")
	}
	if !create && u.Empty() {
		return fmt.Errorf("rcp: an update needs at least one field")
	}
	if !c.IsDestination() && u.TieLineGroup != nil {
		return fmt.Errorf("rcp: tieLineGroup applies to destinations, not %s", c)
	}
	if c.IsFederation() && u.FederationUID != nil {
		return fmt.Errorf("rcp: federationUid links a local IO; a %s entry has none", c)
	}
	if create && u.Virtual != nil && *u.Virtual {
		// "Mutually exclusive with every other field in this body."
		rest := u
		rest.Count, rest.Virtual = nil, nil
		if !rest.Empty() {
			return fmt.Errorf("rcp: virtual IOs are created bare — set the other fields with an update afterwards")
		}
	}
	for name, l := range u.Levels {
		if l.Clear != nil && *l.Clear && (l.Tags != nil || l.TagsInherit != nil || l.Device != nil) {
			return fmt.Errorf("rcp: %s: clear takes no other field", name)
		}
		if l.Tags != nil && l.TagsInherit != nil {
			return fmt.Errorf("rcp: %s: supply tags or tagsInherit, not both", name)
		}
		if l.Tags != nil {
			for _, t := range *l.Tags {
				for _, r := range t {
					if r == '|' {
						return fmt.Errorf("rcp: %s: tag %q contains a pipe", name, t)
					}
				}
			}
		}
		if d := l.Device; d != nil {
			if c.IsDestination() && d.Ignore != nil {
				return fmt.Errorf("rcp: %s: ignore applies to sources", name)
			}
			if !c.IsDestination() && d.Disconnect != nil {
				return fmt.Errorf("rcp: %s: disconnect applies to destinations", name)
			}
			if d.IO != nil && *d.IO != 0 && d.SenderReceiver != nil && *d.SenderReceiver != "" {
				return fmt.Errorf("rcp: %s: a device IO and an IP sender/receiver are mutually exclusive", name)
			}
		}
	}
	return nil
}
