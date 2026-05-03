# Blackmagic HyperDeck

`blackmagic-hyperdeck` implements the Blackmagic HyperDeck Ethernet Protocol.

Source of truth: `../assets/HyperDeckEthernetProtocol.pdf`.

The first implementation slice covers the protocol base:

- TCP text connection on port 9993;
- connection preamble;
- `device info`;
- `slot info`;
- `transport info`;
- `remote`;
- `notify`;
- basic `play` / `stop` / `record` provider behavior.

Consumer and provider are separate packages and must continue to work
independently.
