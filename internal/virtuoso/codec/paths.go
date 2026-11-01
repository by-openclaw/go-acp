package codec

import (
	"fmt"
	"net/url"
	"strings"
)

// Addressing facts of the guide ("HTTP URL", "Authorization").
const (
	MaxSlot       = 8 // extension cards are slots 1..8; 0 is the Element Manager here
	SessionCookie = "NEVION_SESID"
	LoginTarget   = "/api/login"
	LogoutTarget  = "/api/logout"
)

// ProductInfoPath is the identity object's path in the data document.
var ProductInfoPath = []string{"dev", "product_info"}

// ValidSlot reports whether slot addresses a module of the chassis.
func ValidSlot(slot int) bool { return slot >= 0 && slot <= MaxSlot }

func apiBase(slot int) string {
	if slot == 0 {
		return "/api"
	}
	return fmt.Sprintf("/slot/%d/api", slot)
}

// SchemaTarget is the module's schema document.
func SchemaTarget(slot int) string { return apiBase(slot) + "/schema.json" }

// ProductInfoTarget is the module's identity, spelled as measured on the device.
func ProductInfoTarget(slot int) string { return apiBase(slot) + "/data/dev/product_info.json" }

// DataTarget is the object at path; an empty path is the whole document.
func DataTarget(slot int, path []string) string {
	t := apiBase(slot) + "/data"
	for _, seg := range path {
		t += "/" + url.PathEscape(seg)
	}
	return t
}

// PatchTarget is the PATCH that changes the field at path to the wire
// text value: the path of its object, the field as a query attribute.
func PatchTarget(slot int, path []string, value string) string {
	last := len(path) - 1
	return DataTarget(slot, path[:last]) + "?" + EscapeAttr(path[last]) + "=" + EscapeAttr(value)
}

// EscapeAttr percent-encodes a query attribute. The guide encodes a space
// as %20, not as the form encoding's "+".
func EscapeAttr(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// SplitPath turns a --path into segments. A path holding a "/" is split on
// "/" alone, because a collection key may hold dots ("0.0.0.0:0:10"); a
// path without one is split on ".", the form every other connector takes.
func SplitPath(s string) []string {
	s = strings.Trim(strings.TrimSpace(s), "/")
	if s == "" {
		return nil
	}
	if strings.Contains(s, "/") {
		return strings.Split(s, "/")
	}
	return strings.Split(s, ".")
}

// JoinPath is the "/" spelling of a path, which SplitPath takes back
// unchanged for any path of two or more segments.
func JoinPath(path []string) string { return strings.Join(path, "/") }
