package codec

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestCommandLine(t *testing.T) {
	// PDF p.10, "Single line command syntax": command names may be
	// followed by colon-separated parameter/value pairs and newline.
	got := CommandLine("remote", map[string]string{"enable": "true"})
	if got != "remote: enable: true\r\n" {
		t.Fatalf("CommandLine = %q", got)
	}
	if got := CommandLine("ping", nil); got != "ping\r\n" {
		t.Fatalf("CommandLine ping = %q", got)
	}
}

func TestReadResponseSimple(t *testing.T) {
	// PDF p.10, "Successful response codes": 200 ok.
	r := bufio.NewReader(strings.NewReader("200 ok\r\n"))
	resp, err := ReadResponse(r)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Code != 200 || resp.Text != "ok" || !resp.OK() {
		t.Fatalf("response = %+v", resp)
	}
}

func TestReadResponseParams(t *testing.T) {
	// PDF p.16, "Retrieving device information": 204 device info
	// followed by parameter lines and a blank line.
	raw := "204 device info:\r\nprotocol version: 1.14\r\nmodel: HyperDeck Studio\r\nslot count: 2\r\n\r\n"
	resp, err := ReadResponse(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Code != 204 || resp.Text != "device info" {
		t.Fatalf("response header = %+v", resp)
	}
	if resp.Params["model"] != "HyperDeck Studio" || resp.Params["slot count"] != "2" {
		t.Fatalf("params = %#v", resp.Params)
	}
}

func TestWriteResponse(t *testing.T) {
	var b bytes.Buffer
	err := WriteResponse(&b, Response{
		Code: 204,
		Text: "device info",
		Params: map[string]string{
			"model":            "HyperDeck Studio",
			"protocol version": "1.14",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "204 device info:\r\nmodel: HyperDeck Studio\r\nprotocol version: 1.14\r\n\r\n"
	if b.String() != want {
		t.Fatalf("WriteResponse = %q want %q", b.String(), want)
	}
}
