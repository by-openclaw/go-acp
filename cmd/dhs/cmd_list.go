package main

import (
	"fmt"

	"acp/internal/protocol"
)

func runListProtocols() error {
	names := protocol.List()
	if len(names) == 0 {
		fmt.Println("(no protocols registered — this is a build configuration bug)")
		return nil
	}
	for _, name := range names {
		f, err := protocol.Get(name)
		if err != nil {
			continue
		}
		m := f.Meta()
		fmt.Printf("%-8s port=%-5d %s\n", m.Name, m.DefaultPort, m.Description)
	}
	return nil
}
