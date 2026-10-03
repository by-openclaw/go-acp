package main

import (
	"strings"
	"testing"
)

// SW-P-08's extended forms carry 16-bit source and destination fields
// (§3.2.3: DIV 256 / MOD 256), so a matrix may be sized up to 65 535 on
// either axis — the Neuron Shuffle reports 53 190 sources. The 14-bit cap
// this parser used to apply is SW-P-02's (§3.2.47), not this protocol's.
func TestMatrixConfigFlagsTakeTheWiresSixteenBits(t *testing.T) {
	rest, mc, seen, err := extractMatrixConfigFlags([]string{"interrogate", "--srcs", "53190", "--dsts=17728", "--dst", "0"}, nil)
	if err != nil || !seen || mc.Srcs != 53190 || mc.Dsts != 17728 {
		t.Fatalf("mc = %+v seen=%v err=%v", mc, seen, err)
	}
	if strings.Join(rest, " ") != "interrogate --dst 0" {
		t.Errorf("rest = %q", rest)
	}
	_, _, _, err = extractMatrixConfigFlags([]string{"--srcs", "65535", "--dsts", "65535"}, nil)
	if err != nil {
		t.Errorf("65535 is the last id the wire can name: %v", err)
	}
	_, _, _, err = extractMatrixConfigFlags([]string{"--srcs", "65536"}, nil)
	if err == nil || !strings.Contains(err.Error(), "65535") {
		t.Errorf("65536 cannot be encoded and must say so: %v", err)
	}
	_, _, _, err = extractMatrixConfigFlags([]string{"--dsts", "65536"}, nil)
	if err == nil {
		t.Error("65536 destinations accepted")
	}
}
