# Blackmagic HyperDeck Ethernet Protocol

Atomic context for `internal/blackmagic-hyperdeck/`.

Source of truth:

- `assets/HyperDeckEthernetProtocol.pdf`
- Extracted working text: `assets/HyperDeckEthernetProtocol.txt`

Manufacturer is **Blackmagic**. Device/protocol family is **HyperDeck**.
Canonical protocol ID is `blackmagic-hyperdeck`. Alias discussion is deferred.

## Roles

```
consumer/  outbound client/control side
provider/  inbound HyperDeck simulator/server side
codec/     pure text wire grammar
```

Consumer and provider must work independently. Shared wire rules go only in
`codec/` or tiny protocol-local helpers.

## Wire Rules

| Rule | Source |
|---|---|
| Server listens on TCP port 9993 | PDF p.10, Protocol Details, Connection |
| Protocol is line-oriented text | PDF p.10, Protocol Details, Basic syntax |
| Server lines are separated by ASCII CR LF | PDF p.10, Basic syntax |
| Client messages may be separated by LF or CR LF | PDF p.10, Basic syntax |
| Single-line command is `{Command name}` plus optional `: parameter: value` pairs | PDF p.10, Single line command syntax |
| Multiline command syntax exists but first implementation emits single-line commands only | PDF p.10, Multiline command syntax |
| Simple response is `{code} {text}` | PDF p.10, Response syntax |
| Parameter response is `{code} {text}:` followed by `key: value` lines and blank line | PDF p.10, Response syntax |
| `200 ok` is command acknowledgement | PDF p.10, Successful response codes |
| Success responses with parameters are `201` to `299` | PDF p.10, Successful response codes |
| Failure responses are `100` to `199` | PDF p.11, Failure response codes |
| Async responses are `500` to `599` | PDF p.11, Asynchronous response codes |
| On connection server sends `500 connection info` with protocol version and model | PDF p.11, Connection response |
| Too many clients may receive `120 connection failed` and be disconnected | PDF p.12, Connection rejection |
| `quit` cleanly closes the connection | PDF p.12, Closing connection |
| `ping` checks server response only | PDF p.12, Checking connection status |

## Implemented First Slice

The initial connector intentionally implements the stable control/status base
before the full catalogue:

| Command | Consumer | Provider | Source |
|---|---|---|---|
| `device info` | yes | yes | PDF p.16 |
| `slot info` | yes | yes | PDF p.17 |
| `transport info` | yes | yes | PDF p.20 |
| `remote` query/set | yes | yes | PDF p.12 |
| `notify` query/set | yes | yes | PDF p.16 |
| `play` | yes via `transport.status=play` | yes | PDF p.14 |
| `stop` | yes via `transport.status=stopped` | yes | PDF p.14 |
| `record` | yes via `transport.status=record` | yes | PDF p.3 command table |
| `ping` | codec/provider support | yes | PDF p.12 |
| `help` / `?` / `commands` | provider support | yes | PDF p.13 / p.15 |

## Version And Model Compatibility

The PDF is dated December 2024 and names HyperDeck Extreme, Shuttle, and
Studio families on p.1. Device information returns `protocol version`,
`model`, `slot count`, and `software version` (p.16). Treat these as runtime
capability inputs.

If a model/firmware changes response fields, preserve strict parsing for known
fields and keep unknown fields in response maps. Do not silently assume all
models expose the same slots, formats, or timeline features.

If a field cannot be discovered reliably, expose it as config/provider property
instead of hardcoding. This mirrors Probel matrix-size configuration.

## Pending Catalogue

The PDF command table spans pages 2-9. Add remaining commands in small,
consumer+provider pairs with tests and page citations:

- timeline/playrange/goto/jog/shuttle;
- clip list/count/get/add/remove/clear/rebuild;
- disk list and external drives;
- configuration/dynamic range/cache/slate/NAS;
- watchdog.
