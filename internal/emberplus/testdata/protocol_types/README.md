# Ember+ per-type fixtures

One slimmed capture + frozen tshark tree per Glow element type, as defined by
the Glow BER DTD (Ember+ Documentation v2.50, section 5 "The DTD"). The
dissector is expected to render every type exactly as frozen.
`internal/emberplus/consumer/fixture_parity_test.go` checks each folder ships
its three files and that the tree shows the type's APPLICATION tag; byte-exact
parity against a live tshark run is checked by hand when the dissector changes.

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
| 15| 20      | QualifiedFunction       | [`qualified_function/`](qualified_function/)       | 91     |
| 16| 21      | TupleItemDescription    | [`tuple_item_description/`](tuple_item_description/) | 91   |

Rows 1–15 come from Lawo's TinyEmber+ / TinyEmberPlusRouter; row 16 from a
real Lawo Power Core.

## Not covered — no independent source yet (#62)

| APP tag | Type                   | Reason                                                     |
|---------|------------------------|------------------------------------------------------------|
| 12      | StreamDescription      | no device captured so far sends a streamDescriptor         |
| 24      | Template               | no device captured so far exposes templates                |
| 25      | QualifiedTemplate      | as above                                                   |

Checked against every capture in `../fixtures/` (TinyEmber+ router, DHD,
DHD streams, Power Core). A provider that ships these — a Lawo mc² is the
usual one — closes the gap.

## Using a fixture

```bash
export PATH="/c/Program Files/Wireshark:$PATH"   # Windows
tshark -r tests/fixtures/protocol_types/emberplus/matrix/capture.pcapng -V
```

Compare the output to `matrix/tshark.tree` — they should match once volatile
timestamp fields are masked (`scripts/fixturize.sh` handles this on freeze).
