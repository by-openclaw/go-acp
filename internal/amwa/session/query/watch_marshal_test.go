package query

import (
	"context"
	"errors"
	"testing"
)

// Subscribe's encode-error guard is unreachable through any caller input —
// the subscription body is a map of marshalable values — so it is exercised
// through the marshalJSON seam, the same way transport drives its
// raw-socket guards. Without the seam this branch could rot untested.
func TestSubscribeEncodeErrorGuard(t *testing.T) {
	orig := marshalJSON
	marshalJSON = func(any) ([]byte, error) { return nil, errors.New("boom") }
	defer func() { marshalJSON = orig }()

	c := &Client{Base: "http://registry.example/x-nmos/query/v1.3"}
	if _, err := c.Subscribe(context.Background(), SubscribeRequest{ResourcePath: "/nodes/"}); err == nil {
		t.Fatal("Subscribe must surface a body-encode failure")
	}
}
