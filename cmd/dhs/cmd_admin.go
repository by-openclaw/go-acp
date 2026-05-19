package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"

	"dhs/internal/errcode"
	"dhs/internal/provider/admin"
)

// R25 #490 validation codes for the admin CLI dispatcher.
var (
	errAdminSocketUnreachable = errcode.New(errcode.LayerValidation, "admin-socket-unreachable", errcode.ClassUsage)
	errAdminVerbInvalid       = errcode.New(errcode.LayerValidation, "admin-verb-invalid", errcode.ClassUsage)
)

// runProducerAdmin implements `dhs producer <proto> admin <feature> <action> [args]`.
// Dials the producer's local admin socket and submits one JSON
// request. Per R25 #490 spec, the admin socket is local-only — there
// is no network exposure.
//
// v1 dispatches generic feature:action verbs to the running producer.
// The producer registers handlers for whatever it supports today; the
// CLI returns admin:verb-not-implemented for anything the producer
// hasn't wired yet. New verbs land by registering on the producer
// side without changing the CLI dispatcher.
func runProducerAdmin(ctx context.Context, protoName string, args []string) error {
	fs := flag.NewFlagSet("producer "+protoName+" admin", flag.ContinueOnError)
	tagFlag := fs.String("tag", protoName, "connector tag — disambiguates the socket path when several producers run on the same host (e.g. emberplus-9000 vs emberplus-9100)")
	socketPath := fs.String("socket", "", "explicit socket path; overrides --tag-derived default")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 2 {
		return fmt.Errorf("usage: dhs producer %s admin <feature> <action> [params...]", protoName)
	}
	feature := rest[0]
	action := rest[1]

	sock := *socketPath
	if sock == "" {
		sock = admin.DefaultSocketPath(*tagFlag)
	}
	verb := feature + ":" + action

	// Params: each remaining argv element is either `key=value` (parsed
	// into a JSON object) or a bare positional value. If every extra is
	// k=v form the payload is the merged object; if every extra is bare
	// the payload is a JSON array (legacy shape); mixed forms are an
	// operator typo and rejected.
	var params json.RawMessage
	if len(rest) > 2 {
		extras := rest[2:]
		kvCount := 0
		for _, e := range extras {
			if strings.Contains(e, "=") {
				kvCount++
			}
		}
		switch {
		case kvCount == 0:
			// All bare values — legacy array form, used by handlers
			// that already parsed positional argv (none today).
			buf, err := json.Marshal(extras)
			if err != nil {
				return fmt.Errorf("%w: marshal extras: %v", errAdminVerbInvalid, err)
			}
			params = buf
		case kvCount == len(extras):
			// All k=v — build the object the typed handlers expect.
			obj := make(map[string]string, kvCount)
			for _, e := range extras {
				i := strings.Index(e, "=")
				k := strings.TrimSpace(e[:i])
				v := e[i+1:]
				if k == "" {
					return fmt.Errorf("%w: empty key in %q", errAdminVerbInvalid, e)
				}
				obj[k] = v
			}
			buf, err := json.Marshal(obj)
			if err != nil {
				return fmt.Errorf("%w: marshal params: %v", errAdminVerbInvalid, err)
			}
			params = buf
		default:
			return fmt.Errorf("%w: mixed bare + k=v extras: %v (use either all positional or all key=value)",
				errAdminVerbInvalid, extras)
		}
	}

	resp, err := admin.Call(ctx, sock, admin.Request{Verb: verb, Params: params})
	if err != nil {
		return fmt.Errorf("%w: %s: %v", errAdminSocketUnreachable, sock, err)
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	if len(resp.Data) > 0 {
		// Pretty-print so an operator can eyeball the result; admin is
		// a human-driven interface in v1.
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, resp.Data, "", "  "); err != nil {
			fmt.Println(string(resp.Data))
		} else {
			fmt.Println(pretty.String())
		}
	} else {
		fmt.Println("ok")
	}
	return nil
}
