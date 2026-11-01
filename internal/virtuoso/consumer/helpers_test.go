package virtuoso

import (
	"bytes"
	"encoding/json"
	"sync"

	transporthttp "dhs/internal/transport/http"
)

func jsonNumber(s string) json.Number { return json.Number(s) }

func nilClient() *transporthttp.Client { return transporthttp.NewClient() }

// syncBuffer is a log sink read while the poller may still write.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
