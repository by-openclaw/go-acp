package cerebrumnb

import (
	"testing"
)

func TestSplitDevicePath(t *testing.T) {
	cases := []struct {
		in              string
		dev, sub, obj   string
		wantErr         bool
	}{
		{"BOARD-A.SUB1.Status.Connected", "BOARD-A", "SUB1", "Status.Connected", false},
		{"DeviceX.Y.Z", "DeviceX", "Y", "Z", false},
		{"single", "", "", "", true},
		{"only.two", "", "", "", true},
		{".empty.first", "", "", "", true},
		{"empty..middle", "", "", "", true},
	}
	for _, c := range cases {
		dev, sub, obj, err := splitDevicePath(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("splitDevicePath(%q) err=%v wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if c.wantErr {
			continue
		}
		if dev != c.dev || sub != c.sub || obj != c.obj {
			t.Errorf("splitDevicePath(%q) = (%q,%q,%q) want (%q,%q,%q)",
				c.in, dev, sub, obj, c.dev, c.sub, c.obj)
		}
	}
}

func TestSplitURLHostPort(t *testing.T) {
	cases := []struct {
		url      string
		wantHost string
		wantPort int
	}{
		{"ws://10.6.239.116:40007/", "10.6.239.116", 40007},
		{"wss://cerebrum.local:8443/", "cerebrum.local", 8443},
		{"ws://host/", "host", 80},
		{"wss://secure/", "secure", 443},
	}
	for _, c := range cases {
		h, p := splitURLHostPort(c.url)
		if h != c.wantHost || p != c.wantPort {
			t.Errorf("splitURLHostPort(%q) = (%q,%d) want (%q,%d)",
				c.url, h, p, c.wantHost, c.wantPort)
		}
	}
}

func TestProfile_Counts(t *testing.T) {
	p := &Profile{}
	p.Event("a")
	p.Event("a")
	p.Event("b")
	c := p.Counts()
	if c["a"] != 2 || c["b"] != 1 {
		t.Errorf("counts: got %+v want a=2 b=1", c)
	}
}

func TestSession_NextMTID_NeverZero(t *testing.T) {
	s := &Session{}
	s.mtidNext.Store(0xfffffffe)
	for i := 0; i < 5; i++ {
		v := s.nextMTID()
		if v == 0 {
			t.Fatalf("nextMTID returned 0 on iteration %d", i)
		}
	}
}
