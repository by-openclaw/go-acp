package dnssd

import (
	"testing"

	"dhs/internal/amwa/codec/dnssd"
)

// A responder answers for every record it owns (RFC 6762 §6): the PTR
// under the service type, the SRV and TXT under the instance name, the
// A under the host. A resolver that knows the instance and asks for its
// TXT again must be answered, not only one that lists the service.
func TestResponderAnswersForEveryRecordItOwns(t *testing.T) {
	ins := dnssd.Instance{
		Name: "dhs-node", Service: dnssd.ServiceNode, Domain: dnssd.DefaultDomain,
		Host: "node-host.local", Port: 8080,
	}
	for _, tc := range []struct {
		why  string
		q    dnssd.Question
		want bool
	}{
		{"PTR under the service type", dnssd.Question{Name: ins.PTRName(), Type: dnssd.TypePTR}, true},
		{"SRV under the instance name", dnssd.Question{Name: ins.FullName(), Type: dnssd.TypeSRV}, true},
		{"TXT under the instance name", dnssd.Question{Name: ins.FullName(), Type: dnssd.TypeTXT}, true},
		{"TXT, trailing dot and another case", dnssd.Question{Name: "DHS-NODE." + ins.PTRName() + ".", Type: dnssd.TypeTXT}, true},
		{"A under the host", dnssd.Question{Name: "node-host.local", Type: dnssd.TypeA}, true},
		{"ANY under the service type", dnssd.Question{Name: ins.PTRName(), Type: dnssd.TypeANY}, true},
		{"ANY under the instance name", dnssd.Question{Name: ins.FullName(), Type: dnssd.TypeANY}, true},
		{"ANY under the host", dnssd.Question{Name: "node-host.local", Type: dnssd.TypeANY}, true},
		{"PTR under the instance name", dnssd.Question{Name: ins.FullName(), Type: dnssd.TypePTR}, false},
		{"TXT of another instance", dnssd.Question{Name: "other." + ins.PTRName(), Type: dnssd.TypeTXT}, false},
		{"A of another host", dnssd.Question{Name: "elsewhere.local", Type: dnssd.TypeA}, false},
		{"ANY of another name", dnssd.Question{Name: "elsewhere.local", Type: dnssd.TypeANY}, false},
		{"a type it does not hold", dnssd.Question{Name: ins.FullName(), Type: 28}, false},
	} {
		if got := answersQuestion(ins, tc.q); got != tc.want {
			t.Errorf("%s: answered = %v, want %v", tc.why, got, tc.want)
		}
	}
}
