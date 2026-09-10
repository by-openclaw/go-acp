package main

import (
	"fmt"
	"strings"

	"dhs/internal/consumer"
)

func runListProtocols() error {
	names := consumer.List()
	if len(names) == 0 {
		fmt.Println("(no protocols registered — this is a build configuration bug)")
		return nil
	}
	for _, name := range names {
		f, err := consumer.Get(name)
		if err != nil {
			continue
		}
		m := f.Meta()
		fmt.Printf("%-8s port=%-5d %s\n", m.Name, m.DefaultPort, m.Description)
	}
	return nil
}

// printRegisteredProtocols lists what is actually registered, indented for a
// help screen.
//
// `dhs consumer -h` used to carry its own list, and a hardcoded catalogue in
// generic code is exactly what the repository's rules forbid: it goes stale
// the moment a plugin is added, and nothing fails, because the protocol still
// works — it is only undiscoverable. A description is trimmed to keep the
// screen readable; `list-protocols` prints them whole.
func printRegisteredProtocols() {
	const width = 60

	for _, name := range consumer.List() {
		f, err := consumer.Get(name)
		if err != nil {
			continue
		}
		fmt.Printf("  %-13s %s\n", name, shortDescription(f.Meta().Description, width))
	}
}

// shortDescription cuts a registered description down to one clause.
//
// Descriptions are written for `list-protocols`, which prints them whole, so
// they carry the detail a help screen has no room for. Cutting at the first
// semicolon or dash keeps the part that identifies the protocol; closing any
// bracket left open keeps the result from reading as a truncation.
func shortDescription(desc string, width int) string {
	if i := strings.IndexAny(desc, ";—"); i > 0 {
		desc = strings.TrimSpace(desc[:i])
	}
	if len([]rune(desc)) > width {
		desc = strings.TrimSpace(string([]rune(desc)[:width])) + "…"
	}
	if strings.Count(desc, "(") > strings.Count(desc, ")") {
		desc += ")"
	}
	return desc
}
