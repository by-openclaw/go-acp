// tcpdump — tiny raw TCP listener that hex-dumps every byte received.
// Zero framing, zero decoding — pure wire-level capture for the case
// where the real provider/consumer logs say "no frame received" and you
// want to know if anything is arriving at all.
//
// Build-and-run is not required; this file uses `go run`:
//
//	go run scripts/tcpdump.go --port 2008
//
// Each TCP connection gets its own goroutine. Every chunk of bytes the
// client sends is timestamped, decorated with the remote peer address,
// and printed as space-separated lowercase hex on stdout. The server
// sends nothing back — Commie/VSM will eventually time out, which is
// fine for a debug capture.
//
//go:build ignore

package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"
)

func main() {
	port := flag.Int("port", 2008, "TCP port to listen on")
	host := flag.String("host", "0.0.0.0", "TCP host to bind")
	out := flag.String("out", "", "optional file to also write the hex dump")
	flag.Parse()

	addr := fmt.Sprintf("%s:%d", *host, *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}
	defer func() { _ = ln.Close() }()

	var file *bufio.Writer
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			log.Fatalf("create %s: %v", *out, err)
		}
		defer func() { _ = f.Close() }()
		file = bufio.NewWriter(f)
		defer func() { _ = file.Flush() }()
	}

	fmt.Fprintf(os.Stderr, "tcpdump listening on %s — Ctrl-C to stop\n", addr)
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Fatalf("accept: %v", err)
		}
		go handle(c, file)
	}
}

func handle(c net.Conn, file *bufio.Writer) {
	defer func() { _ = c.Close() }()
	peer := c.RemoteAddr().String()
	fmt.Fprintf(os.Stderr, "[%s] CONNECT from %s\n", ts(), peer)

	buf := make([]byte, 4096)
	for {
		n, err := c.Read(buf)
		if n > 0 {
			line := fmt.Sprintf("[%s] %s RX %d bytes: %s",
				ts(), peer, n, hex(buf[:n]))
			fmt.Println(line)
			if file != nil {
				_, _ = fmt.Fprintln(file, line)
				_ = file.Flush()
			}
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] %s CLOSE: %v\n", ts(), peer, err)
			return
		}
	}
}

func ts() string { return time.Now().Format("15:04:05.000") }

func hex(b []byte) string {
	const h = "0123456789abcdef"
	out := make([]byte, 0, len(b)*3)
	for i, x := range b {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, h[x>>4], h[x&0x0f])
	}
	return string(out)
}
