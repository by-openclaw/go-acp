package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"

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

	// Params: rest of argv as a JSON array of strings for simple
	// passthrough (the producer-side handler converts to typed args).
	var params json.RawMessage
	if len(rest) > 2 {
		extras := rest[2:]
		buf, err := json.Marshal(extras)
		if err != nil {
			return fmt.Errorf("%w: --params: %v", errAdminVerbInvalid, err)
		}
		params = buf
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
