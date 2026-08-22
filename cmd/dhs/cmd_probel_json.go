package main

// --output json for the probel-sw08p point-read verbs (#751 G1a):
// interrogate, tally-dump, dual-status, protect-interrogate,
// protect-dump. One JSON object per invocation on stdout, field names
// matching the canonical file grammar (dest/srce/levels — same
// vocabulary as -xpoint.csv), so scripts and the ansible verb role
// parse one shape family across CLI and pack files. Name reads keep
// their CSV path via export (labels are a file-set concern).

import (
	"encoding/json"
	"fmt"

	"dhs/internal/probel-sw08p/codec"
	probelproto "dhs/internal/probel-sw08p/consumer"
)

// emitReadJSON marshals one read result to stdout.
func emitReadJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// probelXpointJSON is one crosspoint in read output — the canonical
// grammar's vocabulary.
type probelXpointJSON struct {
	Dest int `json:"dest"`
	Srce int `json:"srce"`
}

type probelInterrogateJSON struct {
	Matrix int `json:"matrix"`
	Level  int `json:"level"`
	Dest   int `json:"dest"`
	Srce   int `json:"srce"`
}

type probelTallyDumpJSON struct {
	Matrix int                `json:"matrix"`
	Level  int                `json:"level"`
	Form   string             `json:"form"` // "byte" | "word"
	Rows   []probelXpointJSON `json:"rows"`
}

func probelTallyDumpToJSON(res probelproto.TallyDumpResult) probelTallyDumpJSON {
	out := probelTallyDumpJSON{Form: "byte"}
	first, srcs := probelTallyTable(res)
	if res.IsWord {
		out.Form = "word"
		out.Matrix, out.Level = int(res.Word.MatrixID), int(res.Word.LevelID)
	} else {
		out.Matrix, out.Level = int(res.Byte.MatrixID), int(res.Byte.LevelID)
	}
	out.Rows = make([]probelXpointJSON, 0, len(srcs))
	for i, s := range srcs {
		out.Rows = append(out.Rows, probelXpointJSON{Dest: first + i, Srce: s})
	}
	return out
}

type probelDualStatusJSON struct {
	Who         string `json:"who"` // MASTER | SLAVE
	Active      bool   `json:"active"`
	IdleFaulty  bool   `json:"idle_faulty"`
	SlaveActive bool   `json:"slave_active"`
}

type probelProtectJSON struct {
	Matrix int `json:"matrix"`
	Level  int `json:"level"`
	Dest   int `json:"dest"`
	State  int `json:"state"`
	Device int `json:"device"`
}

type probelProtectDumpJSON struct {
	Matrix int                 `json:"matrix"`
	Level  int                 `json:"level"`
	Rows   []probelProtectJSON `json:"rows"`
}

func probelProtectDumpToJSON(res codec.ProtectTallyDumpParams) probelProtectDumpJSON {
	out := probelProtectDumpJSON{Matrix: int(res.MatrixID), Level: int(res.LevelID)}
	out.Rows = make([]probelProtectJSON, 0, len(res.Items))
	for i, it := range res.Items {
		out.Rows = append(out.Rows, probelProtectJSON{
			Matrix: int(res.MatrixID), Level: int(res.LevelID),
			Dest: int(res.FirstDestinationID) + i, State: int(it.State), Device: int(it.DeviceID),
		})
	}
	return out
}
