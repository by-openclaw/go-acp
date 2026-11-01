package codec

import (
	"fmt"
	"net/url"
	"strings"
)

// Invalid is a request refused before it reaches the wire.
type Invalid struct {
	Field  string
	Reason string
}

func (e *Invalid) Error() string { return e.Field + ": " + e.Reason }

// Target is one addressed call: the name, its scope, and the two query
// parameters the page defines.
type Target struct {
	Name  string
	Scope string
	Input string // ?input=N
	Field string // ?field=object.parent.target
}

// ParseTarget reads name, name/scope or name.scope, with an optional
// ?field= or ?input= query. A scope the catalogue does not list is passed
// to the instrument: the page disagrees with itself on some of them.
func ParseTarget(s string) (Target, error) {
	s = strings.TrimSpace(s)
	head, query, hasQuery := strings.Cut(s, "?")
	var t Target
	t.Name = head
	if i := strings.IndexAny(head, "/."); i >= 0 {
		t.Name, t.Scope = head[:i], head[i+1:]
	}
	if t.Name == "" {
		return Target{}, &Invalid{"path", "no call named"}
	}
	if !hasQuery {
		return t, nil
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		return Target{}, &Invalid{"path", err.Error()}
	}
	for k := range q {
		switch k {
		case "field":
			t.Field = q.Get(k)
			if t.Field != "object" && !strings.HasPrefix(t.Field, "object.") {
				return Target{}, &Invalid{"field", `a field path starts with "object"`}
			}
		case "input":
			t.Input = q.Get(k)
		default:
			return Target{}, &Invalid{"path", fmt.Sprintf("unknown query %q (the page defines field and input)", k)}
		}
	}
	return t, nil
}

// String is the call as the operator writes it, and as walk names it.
func (t Target) String() string {
	s := t.Name
	if t.Scope != "" {
		s += "/" + t.Scope
	}
	return s + t.query()
}

// URLPath is the path and query after the host.
func (t Target) URLPath() string {
	s := "/api/" + url.PathEscape(t.Name)
	if t.Scope != "" {
		s += "/" + url.PathEscape(t.Scope)
	}
	return s + t.query()
}

func (t Target) query() string {
	q := url.Values{}
	if t.Input != "" {
		q.Set("input", t.Input)
	}
	if t.Field != "" {
		q.Set("field", t.Field)
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// Check refuses what the page rules out for this call before the wire.
func (e Entry) Check(t Target, write bool) error {
	if write && !e.Mode.CanPost() {
		return &Invalid{t.Name, "the page gives this call GET only"}
	}
	if !write && !e.Mode.CanGet() {
		return &Invalid{t.Name, "the page gives this call POST only"}
	}
	if !write && e.NoRead {
		return &Invalid{t.Name, "not read: the page does not say a GET of this call is harmless, or its answer is no value"}
	}
	if write && t.Field != "" {
		return &Invalid{"field", "a field selects from a GET answer only"}
	}
	if e.Scope == ScopeInput {
		if !write && t.Input == "" {
			return &Invalid{t.Name, "needs ?input=0 .. 5"}
		}
		return nil
	}
	if e.Scope != ScopeNone && !e.ScopeOptional && t.Scope == "" {
		return &Invalid{t.Name, "needs a scope: " + strings.Join(e.Scope.Values(), ", ")}
	}
	return nil
}
