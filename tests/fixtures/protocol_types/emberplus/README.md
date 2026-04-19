# Ember+ per-type fixtures

One slimmed capture + frozen tshark tree per Glow element type, as defined by
the Glow BER DTD (Ember+ Documentation v2.50, section 5 "The DTD"). The
dissector is expected to render every type exactly as frozen — the CI parity
test under `tests/unit/fixture_parity/` asserts that.

## Coverage

| # | APP tag | Glow type             | Fixture dir              | Spec page |
|---|---------|-----------------------|--------------------------|-----------|
| 1 | 0 / 11 / 3 | Root → RootElementCollection → Node | [`root_node/`](root_node/)                       | 87, 93 |
| 2 | 10      | QualifiedNode           | [`qualified_node/`](qualified_node/)               | 87     |
| 3 | 1       | Parameter               | [`parameter/`](parameter/)                         | 85     |
| 4 | 9       | QualifiedParameter      | [`qualified_parameter/`](qualified_parameter/)     | 85     |
| 5 | 13      | Matrix                  | [`matrix/`](matrix/)                               | 88     |
| 6 | 17      | QualifiedMatrix         | [`qualified_matrix/`](qualified_matrix/)           | 88     |
| 7 | 16      | Matrix Connection       | [`matrix_connection/`](matrix_connection/)         | 89     |
| 8 | 18      | Label                   | [`label/`](label/)                                 | 89     |
| 9 | 5 / 6   | StreamEntry / StreamCollection | [`stream_collection/`](stream_collection/) | 93     |
| 10| 2 (cmd=32) | Command — GetDirectory | [`command_get_directory/`](command_get_directory/) | 86     |
| 11| 2 (cmd=30) | Command — Subscribe    | [`command_subscribe/`](command_subscribe/)        | 86     |
| 12| 2 (cmd=31) | Command — Unsubscribe  | [`command_unsubscribe/`](command_unsubscribe/)    | 86     |
| 13| 19 / 22 | Function + Invocation   | [`function_invoke/`](function_invoke/)             | 91     |
| 14| 23      | InvocationResult        | [`invocation_result/`](invocation_result/)         | 92     |

## Not covered (TinyEmber+ / TinyEmberPlusRouter gap)

| APP tag | Type                   | Reason                                       |
|---------|------------------------|----------------------------------------------|
| 12      | StreamDescription      | TinyEmber+ does not publish streamDescriptor |
| 20      | QualifiedFunction      | Only Function + QualifiedNode-wrapped Func   |
| 21      | TupleItemDescription   | Function has no `arguments` metadata         |
| 24      | Template               | TinyEmber+ does not expose templates         |
| 25      | QualifiedTemplate      | As above                                     |

Re-capture required against a Lawo / DHD / Riedel provider that ships these
elements. Tracked in a follow-up issue — see root `agents.md`.

## Using a fixture

```bash
export PATH="/c/Program Files/Wireshark:$PATH"   # Windows
tshark -r tests/fixtures/protocol_types/emberplus/matrix/capture.pcapng -V
```

Compare the output to `matrix/tshark.tree` — they should match once volatile
timestamp fields are masked (`scripts/fixturize.sh` handles this on freeze).
