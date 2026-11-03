package main

import (
	"strconv"
	"strings"

	"dhs/internal/rrcs/codec"
)

// rrcsProps collects repeated --prop NAME=VALUE flags.
type rrcsProps []string

func (p *rrcsProps) String() string     { return strings.Join(*p, ",") }
func (p *rrcsProps) Set(v string) error { *p = append(*p, v); return nil }

// rrcsPropValue types a value the way the lists print it: true and false
// are booleans, whole numbers are integers, the rest is text. A value in
// double quotes is always text.
func rrcsPropValue(v string) codec.Value {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return codec.String(v[1 : len(v)-1])
	}
	switch v {
	case "true":
		return codec.Bool(true)
	case "false":
		return codec.Bool(false)
	}
	if n, err := strconv.ParseInt(v, 10, 32); err == nil {
		return codec.Int(int32(n))
	}
	return codec.String(v)
}

// rrcsWriteToHelp describes the guard every writing verb carries.
const rrcsWriteToHelp = "required to write: repeat the host here, exactly as given. A write is refused without it. On 2026-10-06 a ConfigurationChangeEx edit of an AES67 stream made a production RRCS 9.0 drop the connection and stop answering; configuration writes now use ConfigurationChange and are unproven until one passes on a real RRCS"

// rrcsWriteGuard refuses a write unless the operator named the target a
// second time.
func rrcsWriteGuard(verb, host, writeTo string) error {
	if writeTo == "" {
		return rrcsValErr(verb, "this would WRITE to "+host+": add --write-to "+host+" to confirm the target (see --help), or stay with the dry run")
	}
	if writeTo != host {
		return rrcsValErr(verb, "--write-to "+writeTo+" does not match the host "+host)
	}
	return nil
}
