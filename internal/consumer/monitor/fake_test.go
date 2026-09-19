package monitor

import (
	"context"
	"sync"

	"dhs/internal/consumer"
)

// fakeProto is a programmable consumer.Protocol for tests. GetValue
// returns the value registered for a request's resolved OID/path (via
// its addrKey), SetValue stores it, and every call is counted and
// signalled so a test can wait on the effect without sleeping.
type fakeProto struct {
	mu       sync.Mutex
	vals     map[string]consumer.Value
	getErr   map[string]error
	setErr   map[string]error
	gets     int
	sets     int
	getCalls chan string   // signalled on each GetValue (buffered)
	setCalls chan string   // signalled on each SetValue (buffered)
	setBlock chan struct{} // if non-nil, SetValue waits on it before returning
}

func newFakeProto() *fakeProto {
	return &fakeProto{
		vals:     make(map[string]consumer.Value),
		getErr:   make(map[string]error),
		setErr:   make(map[string]error),
		getCalls: make(chan string, 1024),
		setCalls: make(chan string, 1024),
	}
}

func (f *fakeProto) set(key string, v consumer.Value) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vals[key] = v
}

func (f *fakeProto) Connect(ctx context.Context, ip string, port int) error { return nil }
func (f *fakeProto) Disconnect() error                                      { return nil }
func (f *fakeProto) GetDeviceInfo(ctx context.Context) (consumer.DeviceInfo, error) {
	return consumer.DeviceInfo{}, nil
}
func (f *fakeProto) GetSlotInfo(ctx context.Context, slot int) (consumer.SlotInfo, error) {
	return consumer.SlotInfo{}, nil
}
func (f *fakeProto) Walk(ctx context.Context, slot int) ([]consumer.Object, error) {
	return nil, nil
}

func (f *fakeProto) GetValue(ctx context.Context, req consumer.ValueRequest) (consumer.Value, error) {
	key := addrKey(req)
	f.mu.Lock()
	f.gets++
	v := f.vals[key]
	err := f.getErr[key]
	f.mu.Unlock()
	select {
	case f.getCalls <- key:
	default:
	}
	return v, err
}

func (f *fakeProto) SetValue(ctx context.Context, req consumer.ValueRequest, val consumer.Value) (consumer.Value, error) {
	key := addrKey(req)
	f.mu.Lock()
	f.sets++
	err := f.setErr[key]
	if err == nil {
		f.vals[key] = val
	}
	block := f.setBlock
	f.mu.Unlock()
	select {
	case f.setCalls <- key:
	default:
	}
	if block != nil {
		<-block // hold the wire so a test can exercise a slow write
	}
	return val, err
}

func (f *fakeProto) Subscribe(req consumer.ValueRequest, fn consumer.EventFunc) error { return nil }
func (f *fakeProto) Unsubscribe(req consumer.ValueRequest) error                      { return nil }

func (f *fakeProto) getCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gets
}

func (f *fakeProto) setCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sets
}

// intVal / strVal are value builders for tests.
func intVal(n int64) consumer.Value  { return consumer.Value{Kind: consumer.KindInt, Int: n} }
func strVal(s string) consumer.Value { return consumer.Value{Kind: consumer.KindString, Str: s} }
