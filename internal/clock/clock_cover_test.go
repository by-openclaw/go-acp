package clock

import (
	"context"
	"testing"
	"time"
)

// The system clock's After and Sleep behave like their time-package
// counterparts: a non-positive sleep returns at once with the context's
// state, a cancelled context wins over the timer, and a short sleep
// completes.
func TestSystemClockAfterAndSleep(t *testing.T) {
	c := System()
	select {
	case <-c.After(time.Millisecond):
	case <-time.After(2 * time.Second):
		t.Fatal("After never fired")
	}
	if err := c.Sleep(context.Background(), 0); err != nil {
		t.Errorf("Sleep(0) = %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Sleep(ctx, 0); err == nil {
		t.Error("Sleep(0) on a cancelled ctx must report it")
	}
	if err := c.Sleep(ctx, time.Hour); err == nil {
		t.Error("a cancelled ctx must cut a long sleep short")
	}
	if err := c.Sleep(context.Background(), time.Millisecond); err != nil {
		t.Errorf("Sleep(1ms) = %v", err)
	}
}

// The fake clock's ticker refuses a non-positive period, and its Sleep
// honours a non-positive duration, a cancelled context, and Advance.
func TestFakeTickerPanicsAndSleepBranches(t *testing.T) {
	f := NewFake(time.Unix(0, 0))
	func() {
		defer func() {
			if recover() == nil {
				t.Error("NewTicker(0) must panic")
			}
		}()
		f.NewTicker(0)
	}()
	if err := f.Sleep(context.Background(), 0); err != nil {
		t.Errorf("Sleep(0) = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.Sleep(ctx, time.Second); err == nil {
		t.Error("a cancelled ctx must end a fake sleep")
	}
	done := make(chan error, 1)
	go func() { done <- f.Sleep(context.Background(), time.Second) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.Advance(time.Second)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Sleep after Advance = %v", err)
			}
			return
		default:
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatal("Advance never released the sleeper")
}
