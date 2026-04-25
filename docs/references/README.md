# External References

## ACP protocol specs (in this repo)

| Document                  | Path                                          |
|---------------------------|-----------------------------------------------|
| ACP v1.4 specification   | [docs/protocols/AXON-ACP_v1_4.pdf](../protocols/AXON-ACP_v1_4.pdf) |
| ACP2 specification       | [internal/acp2/assets/acp2_protocol.pdf](../protocols/acp2_protocol.pdf)   |
| AN2 transport spec       | [docs/protocols/an2_protocol.pdf](../protocols/an2_protocol.pdf)     |

## Related protocols (future work)

| Protocol   | Description                           | Link                                                          |
|------------|---------------------------------------|---------------------------------------------------------------|
| Ember+     | Lawo parameter control protocol       | https://github.com/Lawo/ember-plus/tree/master/documentation  |
| NMOS IS-04 | AMWA discovery and registration       | https://specs.amwa.tv/is-04/                                  |
| NMOS IS-05 | AMWA device connection management     | https://specs.amwa.tv/is-05/                                  |

## Wireshark dissectors (in this repo)

| Dissector            | Path                                                |
|----------------------|-----------------------------------------------------|
| ACP1 dissector          | [internal/acp1/wireshark/dhs_acpv1.lua](../../internal/acp1/wireshark/dhs_acpv1.lua) |
| ACP2 dissector          | [internal/acp2/wireshark/dhs_acpv2.lua](../../internal/acp2/wireshark/dhs_acpv2.lua) |
| Ember+ dissector        | [internal/emberplus/wireshark/dhs_emberplus.lua](../../internal/emberplus/wireshark/dhs_emberplus.lua) |
| OSC dissector           | [internal/osc/wireshark/dhs_osc.lua](../../internal/osc/wireshark/dhs_osc.lua) |
| Probel SW-P-08 dissector| [internal/probel-sw08p/wireshark/dhs_probel_sw08p.lua](../../internal/probel-sw08p/wireshark/dhs_probel_sw08p.lua) |

---

Copyright (c) 2026 BY-SYSTEMS SRL - www.by-systems.be
