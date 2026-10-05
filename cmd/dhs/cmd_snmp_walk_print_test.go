package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"dhs/internal/snmp/codec"
)

// A walk prints as it goes. With no object limit a large device takes
// minutes to read, and rows held until the end are a command that looks
// dead for all of them.
func TestSNMPWalkPrintsWhileItIsStillWalking(t *testing.T) {
	var out, progress bytes.Buffer
	bind := codec.VarBind{Name: codec.OID{1, 3, 6, 1, 2, 1, 1, 5, 0}, Value: codec.Value{Type: codec.TypeOctetString, Bytes: []byte("x")}}

	const total = 2*walkProgressEvery + 37
	var seenMidWalk, progressMidWalk int
	n, err := printWalk(&out, &progress, 0, nil, func(fn func(codec.VarBind) error) error {
		for i := 1; i <= total; i++ {
			if err := fn(bind); err != nil {
				return err
			}
			if i == walkFlushEvery {
				seenMidWalk = strings.Count(out.String(), "\n")
			}
			if i == walkProgressEvery {
				progressMidWalk = strings.Count(progress.String(), "\n")
			}
		}
		return nil
	})
	if err != nil || n != total {
		t.Fatalf("walked %d objects, err %v; want %d and nil", n, err, total)
	}
	if seenMidWalk != walkFlushEvery {
		t.Errorf("after %d objects %d rows were printed, want them all — the walk was holding its output", walkFlushEvery, seenMidWalk)
	}
	if progressMidWalk != 1 {
		t.Errorf("after %d objects the running count had been printed %d time(s), want once", walkProgressEvery, progressMidWalk)
	}
	if got := strings.Count(out.String(), "\n"); got != total {
		t.Errorf("%d rows printed for %d objects — the last block was not flushed", got, total)
	}
	if got := strings.Count(progress.String(), "objects so far"); got != 2 {
		t.Errorf("running count printed %d time(s) over %d objects, want 2", got, total)
	}
}

// A limit still stops the walk where the operator said, with what was
// read up to there printed; and the walk's own error is passed on.
func TestSNMPWalkLimitAndError(t *testing.T) {
	bind := codec.VarBind{Name: codec.OID{1, 3, 6, 1, 2, 1, 1, 5, 0}, Value: codec.Value{Type: codec.TypeOctetString, Bytes: []byte("x")}}
	endless := func(fn func(codec.VarBind) error) error {
		for {
			if err := fn(bind); err != nil {
				return err
			}
		}
	}
	var out, progress bytes.Buffer
	n, err := printWalk(&out, &progress, 7, nil, endless)
	if n != 7 || err == nil || !strings.Contains(err.Error(), "--limit of 7 objects") {
		t.Fatalf("limit 7: %d objects, err %v", n, err)
	}
	if got := strings.Count(out.String(), "\n"); got != 7 {
		t.Errorf("%d rows printed at the limit, want 7", got)
	}

	boom := errors.New("agent went away")
	out.Reset()
	n, err = printWalk(&out, &progress, 0, nil, func(fn func(codec.VarBind) error) error {
		_ = fn(bind)
		return boom
	})
	if n != 1 || !errors.Is(err, boom) || strings.Count(out.String(), "\n") != 1 {
		t.Errorf("a walk that fails after one object: n=%d err=%v rows=%d", n, err, strings.Count(out.String(), "\n"))
	}
}
