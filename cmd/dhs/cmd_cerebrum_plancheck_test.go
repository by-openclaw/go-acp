package main

import (
	"errors"
	"strings"
	"testing"

	"dhs/internal/consumer"
)

func TestCerebrumPlanUnknown(t *testing.T) {
	live := &cerebrumState{
		Src: []cerebrumMneRow{{ID: "1"}, {ID: "2"}},
		Dst: []cerebrumMneRow{{ID: "1"}, {ID: "16"}},
		Lvl: []cerebrumMneRow{{ID: "1"}, {ID: "2"}},
	}
	cases := []struct {
		name string
		st   *cerebrumState
		plan cerebrumPlan
		want []string
	}{
		{"every ID exists", live, cerebrumPlan{
			routes:    []routeSpec{{Dest: "16", Srce: "2", Level: "1", SrceLevel: "2"}},
			locks:     []cerebrumLockRow{{Dest: "1", Levels: []string{"1", "2"}}},
			srceLocks: []cerebrumLockRow{{Dest: "2"}},
			src:       []cerebrumMneRow{{ID: "1"}},
			dst:       []cerebrumMneRow{{ID: "16"}},
		}, nil},
		{"route on a missing destination, source and level", live, cerebrumPlan{
			routes: []routeSpec{{Dest: "99", Srce: "7", Level: "5"}},
		}, []string{"destination 99", "source 7", "level 5"}},
		{"a level the plan creates is known to its routes", live, cerebrumPlan{
			routes: []routeSpec{{Dest: "1", Srce: "1", Level: "5"}},
			lvl:    []cerebrumMneRow{{ID: "5"}},
		}, nil},
		{"locks on missing IOs and levels", live, cerebrumPlan{
			locks:     []cerebrumLockRow{{Dest: "40", Levels: []string{"9"}}},
			srceLocks: []cerebrumLockRow{{Dest: "30"}},
		}, []string{"destination 40", "level 9", "source 30"}},
		{"labels on missing IOs", live, cerebrumPlan{
			src: []cerebrumMneRow{{ID: "3"}},
			dst: []cerebrumMneRow{{ID: "2"}},
		}, []string{"source 3", "destination 2"}},
		{"a catalogue the server did not return is not judged", &cerebrumState{}, cerebrumPlan{
			routes: []routeSpec{{Dest: "99", Srce: "7", Level: "5"}},
		}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bad := cerebrumPlanUnknown(c.st, c.plan)
			if len(bad) != len(c.want) {
				t.Fatalf("got %d rows %q, want %d", len(bad), bad, len(c.want))
			}
			for i, w := range c.want {
				if !strings.Contains(bad[i], w+" does not exist") {
					t.Fatalf("row %d: %q, want %q", i, bad[i], w)
				}
			}
		})
	}
}

func TestCerebrumPlanErrorExit2(t *testing.T) {
	err := cerebrumPlanError([]string{"route 99:1:1: destination 99 does not exist"})
	var verr *consumer.ValidationError
	if !errors.As(err, &verr) || exitCode(err) != 2 || !strings.Contains(err.Error(), "nothing sent") {
		t.Fatalf("want a validation refusal, exit 2; got %v", err)
	}
}
