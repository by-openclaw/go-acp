package main

// `dhs consumer virtuoso <verb>` — the Nevion Virtuoso MI chassis and its
// extension cards (REST/JSON). Every verb is a neutral one; this file is
// the connector's help.

const virtuosoHelp = `dhs consumer virtuoso — Nevion Virtuoso MI chassis and cards (REST/JSON)

USAGE
  dhs consumer virtuoso <verb> <host>[:port] [--slot N] [flags]

SLOTS
  0      the Element Manager (the default)
  1..8   the extension cards; a card that does not answer is reported absent

VERBS
  info    <host>                                     every module: identity, or absent
  walk    <host> --slot N [--all] [--path a.b]       schema + data of a module → every field
  get     <host> --slot N --path dev/product_info    one GET, no walk; an object answers its JSON
  set     <host> --slot N --path P --value V         PATCH <object>?<field>=<value>, then read back
  watch   <host> --slot N [--path P1,P2] [--interval D]  polled, at most 5 reads a second
  export  <host> --format json|yaml|csv [--slot N]   the walked tree to a file

PATHS
  A path is the URL path under /data, slashed. A dotted path works when no
  segment holds a dot; a collection key may ("network/routev4_static_coll/0.0.0.0:0:10/gw").
  walk --path is a display filter on dotted segments.

WRITES
  set refuses a read-only field, a key, an object or an RPC method from the
  device's own schema, with nothing sent. No other verb writes anything.
  A chassis in service is someone's production: see internal/virtuoso/docs/runbook.md.

PORTS (HTTP 80, HTTPS 443)

ENVIRONMENT (secrets never as flags)
  DHS_VIRTUOSO_USER / DHS_VIRTUOSO_PASS   login; the cards take a session cookie only
  DHS_VIRTUOSO_TLS=1                      HTTPS (implied by port 443)
  DHS_VIRTUOSO_INSECURE=1                 accept a self-signed certificate
`
