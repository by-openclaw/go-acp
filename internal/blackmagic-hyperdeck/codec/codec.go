// Package codec implements the Blackmagic HyperDeck Ethernet Protocol
// wire grammar.
//
// Source: HyperDeck Ethernet Protocol PDF, December 2024, p.10
// "Protocol Details": TCP/9993, line-oriented text, server lines are
// separated by ASCII CR LF, and client messages may use LF or CR LF.
package codec

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const DefaultPort = 9993

// Response is one HyperDeck server message.
//
// Source: PDF p.10 "Response syntax"; p.11 "Asynchronous response
// codes". Codes 500-599 are asynchronous messages.
type Response struct {
	Code   int
	Text   string
	Params map[string]string
	Lines  []string
}

func (r Response) Async() bool { return r.Code >= 500 && r.Code <= 599 }
func (r Response) OK() bool    { return r.Code >= 200 && r.Code <= 299 }

// FailureError reports a command failure response.
type FailureError struct {
	Code int
	Text string
}

func (e *FailureError) Error() string {
	return fmt.Sprintf("hyperdeck: %03d %s", e.Code, e.Text)
}

// CommandLine formats a single-line command.
//
// Source: PDF p.10 "Single line command syntax": command name, optional
// colon, parameter name/value pairs, then newline.
func CommandLine(name string, params map[string]string) string {
	name = strings.TrimSpace(name)
	if len(params) == 0 {
		return name + "\r\n"
	}
	var b strings.Builder
	b.WriteString(name)
	b.WriteString(":")
	for _, k := range sortedKeys(params) {
		b.WriteByte(' ')
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(params[k])
	}
	b.WriteString("\r\n")
	return b.String()
}

// ReadResponse reads one response or asynchronous message from r.
func ReadResponse(r *bufio.Reader) (Response, error) {
	line, err := readLine(r)
	if err != nil {
		return Response{}, err
	}
	if len(line) < 3 {
		return Response{}, fmt.Errorf("hyperdeck: short response line %q", line)
	}
	code, err := strconv.Atoi(line[:3])
	if err != nil {
		return Response{}, fmt.Errorf("hyperdeck: bad response code %q", line[:3])
	}
	text := strings.TrimSpace(line[3:])
	resp := Response{Code: code, Text: strings.TrimSuffix(text, ":"), Params: map[string]string{}}
	if !strings.HasSuffix(text, ":") {
		return resp, nil
	}
	for {
		body, err := readLine(r)
		if err != nil {
			return Response{}, err
		}
		if body == "" {
			return resp, nil
		}
		resp.Lines = append(resp.Lines, body)
		if k, v, ok := ParseParamLine(body); ok {
			resp.Params[k] = v
		}
	}
}

// WriteResponse serializes one response using server-side CR LF lines.
func WriteResponse(w io.Writer, resp Response) error {
	text := strings.TrimSpace(resp.Text)
	if text == "" {
		text = "ok"
	}
	if len(resp.Params) == 0 && len(resp.Lines) == 0 {
		_, err := fmt.Fprintf(w, "%03d %s\r\n", resp.Code, text)
		return err
	}
	if _, err := fmt.Fprintf(w, "%03d %s:\r\n", resp.Code, text); err != nil {
		return err
	}
	if len(resp.Lines) > 0 {
		for _, line := range resp.Lines {
			if _, err := fmt.Fprintf(w, "%s\r\n", line); err != nil {
				return err
			}
		}
	} else {
		for _, k := range sortedKeys(resp.Params) {
			if _, err := fmt.Fprintf(w, "%s: %s\r\n", k, resp.Params[k]); err != nil {
				return err
			}
		}
	}
	_, err := io.WriteString(w, "\r\n")
	return err
}

func ParseParamLine(line string) (key, value string, ok bool) {
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:i])
	value = strings.TrimSpace(line[i+1:])
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
