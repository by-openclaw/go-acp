package main

import (
	"fmt"
	"strconv"
	"strings"

	"dhs/internal/cerebrum-nb/codec"
	"dhs/internal/consumer"
)

// cerebrumBound is a wanted device value judged against the descriptor the
// server returns with the object (§5.4.3: DATA_TYPE, MIN, MAX, ENUM_LIST).
// The rule is the one export shows and every connector follows: a number
// above MAX is written as MAX, below MIN as MIN, and the run reports it with
// an error code; a value the object cannot hold at all is refused.
type cerebrumBound struct {
	Value   string // what is sent
	Clamped string // non-empty: why Value differs from the wanted one
}

// cerebrumBoundValue judges want against ov. A nil descriptor (the object
// could not be read) leaves the value as it is: nothing is known to judge by.
func cerebrumBoundValue(ov *codec.DeviceObjectValue, want string, isEnum bool) (cerebrumBound, error) {
	b := cerebrumBound{Value: want}
	if ov == nil {
		return b, nil
	}
	switch strings.ToUpper(ov.DataType) {
	case "INTEGER", "FLOAT":
		n, err := strconv.ParseFloat(strings.TrimSpace(want), 64)
		if err != nil {
			return b, &consumer.ValidationError{Field: ov.Object, Reason: fmt.Sprintf("%s object: %q is not a number", ov.DataType, want)}
		}
		if strings.EqualFold(ov.DataType, "INTEGER") && n != float64(int64(n)) {
			return b, &consumer.ValidationError{Field: ov.Object, Reason: fmt.Sprintf("INTEGER object: %q is not a whole number", want)}
		}
		lo, hi, ok := cerebrumRange(ov)
		if !ok {
			return b, nil
		}
		switch {
		case n > hi:
			b.Value, b.Clamped = cerebrumNumber(hi, ov.DataType), fmt.Sprintf("%s above MAX %s", want, cerebrumNumber(hi, ov.DataType))
		case n < lo:
			b.Value, b.Clamped = cerebrumNumber(lo, ov.DataType), fmt.Sprintf("%s below MIN %s", want, cerebrumNumber(lo, ov.DataType))
		}
	case "ENUM":
		// An enumeration is a choice, not a magnitude: a value outside the
		// list is refused, never moved to the nearest item.
		if len(ov.EnumList) == 0 {
			return b, nil
		}
		if isEnum {
			for _, e := range ov.EnumList {
				if e == want {
					return b, nil
				}
			}
			return b, &consumer.ValidationError{Field: ov.Object, Reason: fmt.Sprintf("ENUM object: %q is not one of %s", want, strings.Join(ov.EnumList, ","))}
		}
		i, err := strconv.Atoi(strings.TrimSpace(want))
		if err != nil || i < 0 || i >= len(ov.EnumList) {
			return b, &consumer.ValidationError{Field: ov.Object, Reason: fmt.Sprintf("ENUM object: %q is not an index 0..%d (%s); --is-enum sends the name", want, len(ov.EnumList)-1, strings.Join(ov.EnumList, ","))}
		}
	}
	return b, nil
}

// cerebrumRange reads MIN and MAX. Absent, unreadable or equal bounds carry
// no range (the server sends MIN=MAX on unbounded objects).
func cerebrumRange(ov *codec.DeviceObjectValue) (lo, hi float64, ok bool) {
	lo, e1 := strconv.ParseFloat(strings.TrimSpace(ov.Min), 64)
	hi, e2 := strconv.ParseFloat(strings.TrimSpace(ov.Max), 64)
	return lo, hi, e1 == nil && e2 == nil && lo < hi
}

// cerebrumNumber writes a bound the way the object holds it: the server
// sends "65536.000000" for an INTEGER.
func cerebrumNumber(v float64, dataType string) string {
	if strings.EqualFold(dataType, "INTEGER") {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// cerebrumClampError is the error code of a run that wrote, or would write,
// a bound instead of the wanted value (exit 2): the plan asks for a value the
// object cannot hold.
func cerebrumClampError(object, reason, wrote string, dryRun bool) error {
	verb := "wrote"
	if dryRun {
		verb = "would write"
	}
	return &consumer.ValidationError{Field: object, Reason: fmt.Sprintf("%s: %s %s instead", reason, verb, wrote)}
}
