package main

// `dhs consumer prism <verb>` — the Telestream PRISM waveform monitor
// (REST/JSON). Every verb is the neutral one, served by the registered
// connector; this file only adds the help page.

import (
	"fmt"
)

// runPrism answers the help token and hands every other verb on.
func runPrism(args []string) (handled bool, remaining []string) {
	if len(args) == 0 || isHelpToken(args[0]) {
		fmt.Print(prismHelp)
		return true, nil
	}
	return false, args
}

const prismHelp = `dhs consumer prism — Telestream PRISM waveform monitor (REST/JSON)

USAGE
  dhs consumer prism <verb> <host>[:port] [flags]

VERBS (served by the registered connector)
  info    <host>                         identity: version, serial, status, PTP, IP signal
  walk    <host> [--path CALL[.SCOPE]]   every GET-able call, every scope, one at a time; a call
                                         that fails is recorded and the walk goes on
  get     <host> --path CALL[/SCOPE][?field=object.a.b]
  set     <host> --path CALL[/SCOPE] --value V   POST, then read back
  watch   <host> [--path CALL[,CALL]] [--interval D]   polls: the page documents no push channel
  export  <host> --format json|yaml|csv  the walk to a file

  get --path waveform_mode/tile1
  get --path "system_status?field=object.fans"
  get --path "inputConfigure?input=3"
  set --path waveform_mode/tile1 --value WAVEFORM_MODE_RGB
  set --path activeInput --value '{"input":2}'

VALUES
  ints with options   an option name, or an integer
  several values      comma-separated: --value "0.4,194.5 mV"
  object              JSON text; it travels under "object"

PORT
  9000 by default. The page names 9000 and links port 80: give <host>:80 if 9000 does not answer.

ENVIRONMENT
  DHS_PRISM_PASS=<password>     sent as "Authorization: Basic <password>", in clear over HTTP
  DHS_PRISM_SCALAR=array|bare   how one number travels: [3] (default) or 3
  DHS_PRISM_ANSWER_WAIT=DUR     one answer's wait in walk and watch (default 5s); --timeout elsewhere
  DHS_PRISM_ALLOW_ACTIONS=1     lets set send the guarded calls (reboot, power off, serial, ...)

Never tested on a real PRISM. See internal/prism/docs/runbook.md.
`
