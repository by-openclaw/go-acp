package probelsw08p

import (
	"dhs/internal/probel-sw08p/codec"
)

// allNames answers an "all names" request with every name of the table:
// one response message when they fit in one, else as many as it takes,
// each carrying up to perMsg names from an ascending first id. SW-P-08
// §3.1.18 / §3.1.20 / §3.1.24: "the controller will respond with one or
// more … RESPONSE messages". Only the first message used to be sent — a
// controller reading a 64-source matrix was given 16 of its labels.
//
// frame builds the response for the names starting at first; nameAt is the
// label of one id.
func allNames(count, perMsg int, nameAt func(i int) string, frame func(first int, names []string) codec.Frame) handlerResult {
	page := func(first int) codec.Frame {
		n := perMsg
		if first+n > count {
			n = count - first
		}
		names := make([]string, n)
		for i := range names {
			names[i] = nameAt(first + i)
		}
		return frame(first, names)
	}
	if count <= perMsg {
		reply := page(0)
		return handlerResult{reply: &reply}
	}
	return handlerResult{streamToSender: func(emit func(codec.Frame) error) error {
		for first := 0; first < count; first += perMsg {
			if err := emit(page(first)); err != nil {
				return err
			}
		}
		return nil
	}}
}
