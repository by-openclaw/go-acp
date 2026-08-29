# NMOS runbook — the three roles, end to end

Every NMOS deployment is three roles. `dhs` is all three, and this page
is how you drive each one from a terminal.

| Role | What it is | Command |
|---|---|---|
| **Node** | a device describing itself | `dhs producer nmos serve` |
| **Registry** | the catalogue devices register into | `dhs registry nmos serve` |
| **Controller** | the operator side — reads, then routes | `dhs consumer nmos …` |

Version support is **v1.0, v1.1, v1.2 and v1.3 in parallel**, on every
role. Nothing downgrades silently: pin one with `--api-ver` when you
need to reproduce a specific device's behaviour.

---

## 1. Look at a device without setting anything up

The fastest useful thing. No Registry, no mDNS, no config:

```bash
dhs consumer nmos walk --node http://10.6.255.102:3000
```

```
Node http://10.6.255.102:3000  (IS-04 v1.3, spec v1.3.3)

  nodes      1
  devices    1
  sources    208
  flows      208
  senders    208
  receivers  208
```

Add `-l` to list every resource with its UUID, label and transport, or
`--json` to pipe the whole catalogue somewhere.

Pin a minor to see what an older controller would see:

```bash
dhs consumer nmos walk --node http://10.6.255.102:3000 --api-ver v1.0 -l
```

**Why `--node` and not `--registry`:** a Registry is a catalogue of many
devices; a Node is one device describing itself. `--node` is the only
way to reach a device that has not registered anywhere — which is the
normal state of a device on a segment that cannot route back to the
Registry.

---

## 2. Route something

Resources are addressed by **UUID**, never by label. Labels are mutable
and non-unique in NMOS, so routing by label moves the wrong signal the
first time two receivers share a name. `walk -l` prints the UUIDs.

**Always dry-run first.** Routing moves real signal and IS-05 has no
undo:

```bash
dhs consumer nmos connect --node http://10.6.255.102:3000 \
  --receiver 03c525d7-c27b-4ba0-8a12-2e25b50e99f7 \
  --sender   00c466fc-23bf-43b9-8139-4d7c0179af7c \
  --dry-run
```

It prints the resolved IS-05 endpoint, what the receiver is doing
**now**, and the exact PATCH body — including the SDP fetched from the
sender. Drop `--dry-run` to send it.

Disconnect:

```bash
dhs consumer nmos connect --node http://10.6.255.102:3000 \
  --receiver 03c525d7-c27b-4ba0-8a12-2e25b50e99f7 --disconnect
```

Scheduled activation (TAI, `<secs>:<nanos>` — **not** Unix time; TAI is
UTC + 37 leap seconds):

```bash
dhs consumer nmos connect ... --mode activate_scheduled_absolute --when 1800000037:0
```

### Two failures worth knowing about

- **`master_enable=false` after a successful-looking connect.** The
  route staged and no signal will flow. `connect` prints a WARNING and
  fires `nmos_is05_master_enable_ignored` rather than reporting success.
- **A sender that serves no transport file.** Not fatal — the receiver
  is still pointed at the sender by id — but you get
  `nmos_is05_no_transport_file`, because a receiver with no SDP often
  will not lock.

---

## 3. Make a Sender actually emit

`connect` points a Receiver at a Sender. It does **not** give the Sender
somewhere to send. A device can be fully connected and move nothing.

The EVS Neuron ships in exactly that state — every Sender enabled, real
source IPs on both ST 2022-7 legs, and no destination:

```
leg 0  src=10.6.40.51   dst=0.0.0.0  port=12700  rtp=true
leg 1  src=10.7.40.51   dst=0.0.0.0  port=12700  rtp=true
```

Its SDP says the same thing (`c=IN IP4 0.0.0.0/32`). Assign the groups:

```bash
dhs consumer nmos set --node http://10.6.255.102:3000 \
  --sender 00c466fc-23bf-43b9-8139-4d7c0179af7c \
  --destination 239.100.40.51,239.101.40.51 --dry-run
```

**One `--destination` per transport leg, in device order.** ST 2022-7
legs run on two separate networks and MUST NOT share a group — that is
the whole point of seamless protection — so a single value applied to
both would be wrong, and a count that does not match the device is
refused rather than silently truncated.

`--port` takes a list the same way. `--enable` / `--disable` sets
`master_enable` alongside. Anything you do not name is left as the
device has it: IS-05 PATCH is a merge, and the transport_params array is
always sent full-length because IS-05 matches legs **positionally**.

Drop `--dry-run` to apply. The Sender's `/active` state and its SDP both
update:

```
SET sender 2c47bf5e-… via http://127.0.0.1:8080/x-nmos/connection/v1.2
  master_enable true
  leg 0  src=127.0.0.1  dst=239.50.0.1  port=5004  rtp=false
```

A leg still reading `0.0.0.0` after a set emits nothing; the command
prints a WARNING and fires `nmos_is05_destination_ignored` rather than
reporting success.

### Ship a device that boots configured

A bundle can seed IS-05 boot state per endpoint id — a `connection`
block beside the IS-04 sections:

```json
"connection": {
  "senders": {
    "<sender-uuid>": {
      "master_enable": true,
      "transport_params": [
        {"rtp_enabled": true, "destination_ip": "239.20.1.1", "destination_port": 5004},
        {"rtp_enabled": true, "destination_ip": "239.22.1.1", "destination_port": 5004}
      ]
    }
  }
}
```

Two legs mean an ST 2022-7 pair (endpoint and constraints grow to
match). `master_enable: true` promotes the endpoint at boot through the
same path a controller activation takes: ACTIVE goes concrete, a Sender
gets its SDP, IS-04 `subscription.active` reflects it. Unknown endpoint
ids, unknown parameter keys, and >2 legs are refused at load — a typo
here otherwise surfaces four suites away as a constraints mismatch.

The test fixture carries four archetypes to copy from:
`dhs-sender-min` / `dhs-receiver-min` (required fields only, IS-05
factory-fresh) and `dhs-sender-full` / `dhs-receiver-full` (every
optional field, BCP-004 caps, boot-active 2022-7 multicast).

## 4. Run a Registry and register a Node into it

Two terminals, both local. This is the loop to reach for when you want
to reproduce a plant on one machine.

```bash
dhs registry nmos serve --bind 0.0.0.0:8235 --no-mdns
```

```bash
dhs producer nmos serve --bind 0.0.0.0:8080 --no-mdns \
  --registry http://127.0.0.1:8235 \
  --advertise-host 127.0.0.1:8080 \
  --config tests/fixtures/nmos/amwa-test-node.json
```

Then look at it through the Registry:

```bash
dhs consumer nmos walk --registry http://127.0.0.1:8235 -l
```

Routing through the Registry works the same as routing at a Node —
`connect` finds the device's IS-05 endpoint from the catalogue:

```bash
dhs consumer nmos connect --registry http://127.0.0.1:8235 \
  --receiver <uuid> --sender <uuid>
```

After an activation the **Node and the Registry agree**: both report
`subscription.sender_id` and `active: true`. If they ever disagree,
that is a bug — IS-04 §4.2 requires the Node to re-POST a changed
resource, and a Controller renders routing state from the Registry.

> **`--advertise-host` does not rewrite the control hrefs.** They come
> from `api.endpoints[0].host` in the bundle. If a device's IS-05
> endpoint resolves to an address you cannot reach, that field is why.

### Two registry flags the field taught us to need

- `--page-limit-default 1000` — first-page-only controllers exist
  (Cerebrum's Network Media reads page one per collection and never
  follows `Link`); on a plant larger than one page they silently lose
  everything registered before the newest device. Raising the
  no-param default is spec-legal; explicit client limits still win.
- `--instance-name` — DNS-SD peers key stored server entries on the
  announced label; if a peer cached a stale/poisoned resolution for
  the old name, republish under a fresh one.

And two peer-side checks before debugging anything else: resolve your
own SRV target from ANOTHER host (a loopback A-record in your announce
makes peers connect to themselves, silently — it happened to us), and
on Cerebrum, verify which Query Server the **Active checkbox** selects
(see `cerebrum-interop.md`).

---

## 5. Discovery modes

All four IS-04 deployment modes are supported. mDNS is the default and
the one most likely to be blocked on a customer network.

| Mode | When | Flags |
|---|---|---|
| A — mDNS + Registry | greenfield, spec-compliant peers | default |
| B — fixed Registry URL | multicast blocked, address known | `--no-mdns --registry http://host:port` |
| B — unicast DNS-SD | multicast blocked, DNS zone available | `--no-mdns --unicast --resolver IP[:port] --domain <zone>` |
| C — direct Node | no Registry at all | `--node http://host:port` |
| D — mDNS peer-to-peer | Cerebrum P2P | `--mdns --no-registry` |

**mDNS does not cross a routed hop.** If the Registry and the device
are on different subnets, mDNS will never find it no matter how the
firewall is configured — use mode B.

Unicast DNS-SD (IS-04 §3.1) wants one PTR + SRV + TXT + A set per
Registry in a conventional zone. A minimal dnsmasq zone publishing a
Registry at 10.100.0.5:8080 under `nmos.lab`:

```
port=8053
no-resolv
no-hosts
host-record=cer.nmos.lab,10.100.0.5
ptr-record=_nmos-register._tcp.nmos.lab,cerebrum._nmos-register._tcp.nmos.lab
srv-host=cerebrum._nmos-register._tcp.nmos.lab,cer.nmos.lab,8080
txt-record=cerebrum._nmos-register._tcp.nmos.lab,"api_ver=v1.1,v1.2,v1.3","api_proto=http","api_auth=false","pri=0"
```

The Node re-asks the zone every 60 s and applies the SAME priority
selection and failover (§6.1) as mDNS — one selection rule, two
transports. All three registration paths were proven live against the
Cerebrum registry in one session: `dhs-node-mdns` (browse, picked
pri=0 over a pri=199 dev registry, then suspended its own
`_nmos-node` announce per §4.2.1), `dhs-node-dnssd` (the zone above),
`dhs-node-manual` (fixed URL).

---

## 6. When something is wrong

Every command prints a compliance summary to stderr, collapsed by
distinct message so one systematic deviation across 208 resources
appears once with a count, not 208 times:

```
2 compliance event(s), 1 distinct:
  x2    [warn] nmos_is04_schema_deviation: peer sender does not match AMWA IS-04 v1.3.3: /id: pattern: ...
```

`nmos_is04_schema_deviation` means the peer's payload fails **AMWA's own
published schema** for that version — those schemas ship in
`internal/amwa/codec/is04/schemas/` and are the validation authority.
The deviation is reported, never fatal: refusing the payload would cost
you the resource and tell you nothing you can act on.

Reading a device is deliberately tolerant. **Emitting** is strict — our
own Node will refuse to serve a payload AMWA's schema rejects.

---

## 7. Conformance

The Node is scored by the AMWA NMOS Testing Tool, once per IS-04 minor,
from the Linux control node — never from Windows:

```bash
cd ansible && ansible-playbook -i inventory/hosts.ini playbooks/amwa-conformance.yml
```

Results land in `tests/integration/nmos/amwa/results/`.

The node and the tool share one bridge on purpose: IS-04 discovery is
mDNS, and testing across a routed boundary silently skips the discovery
half of IS-04-01 and reports a better score for a worse implementation.

**The tool has two mutually exclusive modes, and each suite family
needs its own.** Node suites want the BRIDGED tool (`nmos-testing`),
which shares docker DNS with the node — the node's `manifest_href`
carries the `dhs-node` hostname, and the LAN-mode tool cannot resolve
it (`test_20_01` / IS-05-02 `test_13` fail with NameResolutionError).
Registry suites want the HOST-NETWORK tool (`nmos-testing-lan`), which
participates in real LAN multicast — the bridged tool cannot see the
registry's mDNS announcement (IS-04-02 `test_01/02` fail). The two
cannot run at once (both claim :5000), and a sweep launched with the
wrong one up fails its docker-start task at ~7 tasks in — check the
PLAY RECAP's `ok=` count before believing any scoreboard, because a
crashed play leaves the previous run's results on disk.

The **Registry** (`results-registry/`) and **Controller**
(`results-controller/`) evidence sets have their own READMEs, each
documenting the hermeticity rules its numbers depend on — the registry
one in particular: LAN-mode tool for the mDNS tests, a pre-created
persistent subscription per minor, and no live devices churning the
store during scoring.
