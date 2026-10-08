package main

import (
	"strings"
	"testing"
)

// A benchmark in which operations failed is a failed run. It exited 0
// whatever happened: 4 000 unanswered interrogates read as success.
func TestBenchFailsWhenOperationsFailed(t *testing.T) {
	if err := benchFailures(map[string]*phaseResult{}); err != nil {
		t.Errorf("no phase run: %v", err)
	}
	if err := benchFailures(map[string]*phaseResult{
		"interrogate": {ops: 4000}, "connect": {ops: 4000},
	}); err != nil {
		t.Errorf("a clean run: %v", err)
	}
	err := benchFailures(map[string]*phaseResult{
		"interrogate": {ops: 4000, errs: 4000}, "connect": {ops: 10, errs: 2},
	})
	if err == nil {
		t.Fatal("4 002 failed operations ended as success")
	}
	for _, want := range []string{"4002 of 4010", "interrogate 4000", "connect 2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q is not in the verdict: %v", want, err)
		}
	}
	if err := benchFailures(map[string]*phaseResult{"connect": {ops: 5, errs: 1}}); err == nil || strings.Contains(err.Error(), "interrogate") {
		t.Errorf("one phase run, one failure: %v", err)
	}
}
