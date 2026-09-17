// TAI timestamps -- NMOS-wide, not the property of any one spec.
//
// IS-04 resource versions, IS-05 activation times, IS-07 event timing,
// and IS-08 activation times are all `<seconds>:<nanoseconds>` on the
// TAI scale. They are compared against each other across APIs, so they
// have to come from ONE implementation: an IS-05 activation stamped on
// one epoch and an IS-07 event stamped on another are 37 seconds apart
// for no reason a reader could ever guess, and a controller
// correlating them draws the wrong conclusion.

package spec

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TAILeapSeconds is TAI − UTC: the leap seconds inserted since the two
// scales were aligned in 1972.
//
// 37 since 2017-01-01, and unchanged since — the IERS has announced no
// leap second in the years around this table, and the 2022 CGPM
// resolution is to stop inserting them by 2035. A constant is honest
// for the deployment window; a table would pretend to a precision
// nothing here uses.
//
// Not cosmetic. A controller scheduling an absolute activation sends a
// TAI instant, and reading it as Unix seconds puts the switch 37
// seconds into the future — which looks exactly like a device that
// never activates at all.
const TAILeapSeconds = 37

// FormatTAI renders a wall-clock time as a `<sec>:<nsec>` TAI string.
func FormatTAI(t time.Time) string {
	return fmt.Sprintf("%d:%d", t.Unix()+TAILeapSeconds, t.Nanosecond())
}

// TAIToTime converts a TAI instant to wall-clock time. Inverse of
// FormatTAI.
func TAIToTime(sec, nsec int64) time.Time {
	return time.Unix(sec-TAILeapSeconds, nsec)
}

// ParseTAI reads a `<sec>:<nsec>` string into its two components.
// Reports false on anything that is not that shape.
func ParseTAI(v string) (sec, nsec int64, ok bool) {
	before, after, found := strings.Cut(v, ":")
	if !found {
		return 0, 0, false
	}
	s, err := strconv.ParseInt(before, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	n, err := strconv.ParseInt(after, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return s, n, true
}
