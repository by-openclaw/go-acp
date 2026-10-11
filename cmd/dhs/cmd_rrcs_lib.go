package main

import rrcs "dhs/internal/rrcs/consumer"

// The names below are what the rrcs verbs of this command used before
// their logic moved into the rrcs library. Each one stands for the
// library's own, so the verbs and their tests read as they did while the
// move goes on part by part. A name leaves this file when its last user
// here is gone.

type rrcsSpecProp = rrcs.SpecProp

func rrcsSpecFor(object, block, field string) rrcsSpecProp {
	return rrcs.SpecFor(object, block, field)
}
