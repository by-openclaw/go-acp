# AMWA NMOS — the controller (`dhs consumer nmos`)

One page per verb: what it is for, the form to type, and a run. Every
flag is in [`docs/cli.md`](../../../docs/cli.md); tasks that chain
several verbs are in [`runbook.md`](runbook.md).

Runs marked **captured** were made with the released binary (v0.35.0,
2026-10-04) against the peer named. A verb with no captured run has not
been run against a peer yet, and says so.

Resources are named by IS-04 UUID everywhere: NMOS labels are mutable
and not unique. `walk -l` prints the ids.

## walk — read a catalogue

One device (its own Node API), or a Registry (its Query API).

```
dhs consumer nmos walk --node http://<device>:<port> [-l | --json]
dhs consumer nmos walk --registry http://<registry>:<port> [-l | --json]
```

Captured, the Neuron CONVERT:

```
$ dhs consumer nmos walk --node http://10.6.255.102:3000
Node http://10.6.255.102:3000  (IS-04 v1.3, spec v1.3.3)

  nodes      1
  devices    1
  sources    176
  flows      176
  senders    176
  receivers  176

  -l lists every resource; --json emits the whole catalogue
```

Captured, `-l` on the nmos-cpp reference node, cut down to the node, the
device and five of its controls — the Device's `controls` are where every
other verb finds its endpoint:

```
NODE      1a7740be-1338-5f65-b639-263b508cfa62  dhs-amwa-reference-node
          href=http://10.6.250.104:8120/

DEVICE    8b13b278-6b39-58ae-ba1e-2b9115d5d3d8  dhs-amwa-reference-node
          control  urn:x-nmos:control:sr-ctrl/v1.2                http://10.6.250.104:8120/x-nmos/connection/v1.2
          control  urn:x-nmos:control:events/v1.0                 http://10.6.250.104:8120/x-nmos/events/v1.0
          control  urn:x-nmos:control:cm-ctrl/v1.0                http://10.6.250.104:8120/x-nmos/channelmapping/v1.0
          control  urn:x-nmos:control:ncp/v1.0                    ws://10.6.250.104:8122/x-nmos/ncp/v1.0
          control  urn:x-nmos:control:configuration/v1.0          http://10.6.250.104:8120/x-nmos/configuration/v1.0
```

A Registry serves by default only what is registered at the minor it is
asked at. On v0.35.0 a v1.3 walk of the plant registry therefore shows
24 of its 25 Nodes — the FusioN registers at v1.2. Reading the lower
minors too (`query.downgrade`) is #1330, not released yet; until then
`--api-ver v1.2` shows the v1.2 Nodes alone.

## watch — follow a Registry

```
dhs consumer nmos watch --registry http://<registry>:<port>
```

Captured, the nmos-cpp registry:

```
Registry http://10.6.250.104:8110 (api_ver=v1.3, spec=v1.3.3)
Subscribed /nodes -> ws://10.6.250.104:8111/x-nmos/query/v1.3/subscriptions/8accb863-…
Watching. The first grain is the current state; later grains are changes.
modified nodes        0cda2a74-84c1-5087-88d1-a9354ff14871 dhs-amwa-reference-registry
modified nodes        1a7740be-1338-5f65-b639-263b508cfa62 dhs-amwa-reference-node
```

## connect — route a Sender to a Receiver (IS-05)

```
dhs consumer nmos connect --node|--registry … --receiver <uuid> --sender <uuid> [--dry-run]
dhs consumer nmos connect … --receiver <uuid> --disconnect
dhs consumer nmos connect … --route <receiver>=<sender> [--route …] | --routes <file>
```

`--dry-run` first: routing moves real signal and IS-05 has no undo.
Captured, the nmos-cpp reference node (the SDP, and the receiver id in
the PATCH line, are shortened here):

```
$ dhs consumer nmos connect --node http://10.6.250.104:8120 \
      --receiver 6fbe0f69-9f18-5697-82c4-8e1299d7cd5a --sender 40af44c4-3dd3-53c0-9c54-e2aec5c7f1fd --dry-run
DRY RUN — nothing was sent

would CONNECT via http://10.6.250.104:8120/x-nmos/connection/v1.2
  receiver      6fbe0f69-9f18-5697-82c4-8e1299d7cd5a
  currently     sender=(none) master_enable=false
  PATCH http://10.6.250.104:8120/x-nmos/connection/v1.2/single/receivers/6fbe0f69-…/staged
  {
    "activation": { "mode": "activate_immediate" },
    "master_enable": true,
    "sender_id": "40af44c4-3dd3-53c0-9c54-e2aec5c7f1fd",
    "transport_file": { "data": "v=0\r\no=- 4000050847 …", "type": "application/sdp" }
  }
```

A route the Receiver cannot take is refused before anything is sent —
captured, a WebSocket data Receiver offered an MXL video Sender:

```
error: nmos connect: receiver 4ad1ff0a-bc3c-57d2-a8fb-2272382b577c cannot take sender 6b3dd76e-fdb4-5be4-a245-6d8cce5da772:
  the receiver takes websocket, the sender emits mxl; the receiver is data, the flow is video;
  the flow is video/v210, the receiver takes application/json (--force sends it anyway)
```

(One line on the terminal; wrapped here.)

`--force` sends it anyway and records that it was forced.

## set — configure a Sender's transport (IS-05)

```
dhs consumer nmos set --node|--registry … --sender <uuid> --destination <ip>[,<ip>] [--port n[,n]] [--enable|--disable] [--dry-run]
```

One `--destination` per transport leg. Captured, the refusal on a
two-leg (ST 2022-7) Sender given one:

```
error: nmos set: sender 40af44c4-3dd3-53c0-9c54-e2aec5c7f1fd has 2 transport leg(s), you gave 1 --destination value(s); give one per leg (ST 2022-7 legs must not share a group)
```

## discover — find a Registry by DNS-SD

```
dhs consumer nmos discover [--unicast --resolver <ip> --domain <zone>] [--service <type>]
```

Captured, mDNS on the lab link:

```
Discovered 1 instance(s) of _nmos-register._tcp:
  dhs-nmos-registry._nmos-register._tcp.local
    host = dhs-debian.local:8235
    pri  = 0
    proto= http
    ver  = v1.0,v1.1,v1.2,v1.3
    auth = false
```

## map — read or change a Device's audio channel map (IS-08)

```
dhs consumer nmos map --node|--registry … --device <uuid>
dhs consumer nmos map … --device <uuid> --route <output>:<channel>=<input>:<channel> [--route …] [--dry-run]
dhs consumer nmos map … --device <uuid> --route <output>:<channel>=          # leave it unrouted
dhs consumer nmos map … --device <uuid> --cancel <activation-id>
```

Routes are checked against what the Device declares before anything is
sent; an applied map is read back from `/map/active`. **Not released,
and not run against a Device yet.**

## compat — why a Sender and a Receiver do not agree (IS-11)

```
dhs consumer nmos compat --node|--registry … --receiver <uuid>
dhs consumer nmos compat … --sender <uuid> [--constraints <file> | --release] [--dry-run]
```

Prints the state with the Device's own reason. **Not released, and not
run against a Device yet.**

## config — a Device's model, backup and restore (IS-14)

```
dhs consumer nmos config --node|--registry … --device <uuid>                       # role paths
dhs consumer nmos config … --device <uuid> --role-path <p>                         # describe
dhs consumer nmos config … --role-path <p> --get <id> | --set <id>=<json> | --invoke <id> [--args <json>]
dhs consumer nmos config … --role-path <p> --backup <file> | --restore <file> [--validate-only] [--rebuild]
```

A set is read back; a restore is validated by the Device first and
applied only when every object validates. **Not released, and not run
against a Device yet.**

## events, system, export, audit, probe, registers, facade

In [`docs/cli.md`](../../../docs/cli.md). None has a captured run in
this page yet.
