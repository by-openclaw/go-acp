package dnssd

import (
	"reflect"
	"testing"
)

// The goodbye for a TXT record about to be replaced: that record alone,
// under the instance name, with the data being withdrawn, TTL 0 (RFC
// 6762 §10.1) and no cache-flush bit — it takes one record out of the
// caches, it does not speak for the rrset.
func TestEncodeTXTGoodbyeWithdrawsTheReplacedRecord(t *testing.T) {
	ins := fullInstance()
	ins.TXT = map[string]string{TXTKeyAPIVer: "v1.2", TXTKeyVerSlf: "3"}

	wire, err := EncodeTXTGoodbye(ins)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Decode(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Header.IsResponse() || len(m.Answers) != 1 {
		t.Fatalf("response=%v answers=%d, want a response carrying one record", m.Header.IsResponse(), len(m.Answers))
	}
	rr := m.Answers[0]
	if rr.Type != TypeTXT || rr.Name != ins.FullName() {
		t.Errorf("record %d under %q, want the TXT under %q", rr.Type, rr.Name, ins.FullName())
	}
	if rr.TTL != 0 {
		t.Errorf("TTL = %d, want 0", rr.TTL)
	}
	if rr.Class != ClassIN {
		t.Errorf("class = %#x, want IN without the cache-flush bit", rr.Class)
	}
	want, _ := EncodeTXT(ins.TXT)
	if !reflect.DeepEqual(rr.TXT, want) {
		t.Errorf("TXT = %v, want the withdrawn data %v", rr.TXT, want)
	}

	// Nothing to name it by, or data that cannot be encoded: no packet.
	for name, mutate := range map[string]func(*Instance){
		"no name":    func(i *Instance) { i.Name = "" },
		"no service": func(i *Instance) { i.Service = "" },
		"bad TXT":    func(i *Instance) { i.TXT = map[string]string{"a=b": "x"} },
	} {
		bad := fullInstance()
		mutate(&bad)
		if _, err := EncodeTXTGoodbye(bad); err == nil {
			t.Errorf("%s: a goodbye was built", name)
		}
	}
}
