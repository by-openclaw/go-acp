# AMWA NMOS — reference catalogue

NMOS is a suite of open broadcast IP networking specifications from
**AMWA** (Advanced Media Workflow Association). Each spec defines its
own role pair (provider + consumer); a fully compliant
implementation MUST implement BOTH sides of every spec it claims.

## Roles

| Role | Meaning |
|---|---|
| **provider (device)** | Originates resources / events that consumers register against. |
| **Consumer (controller)** | Discovers + drives subscribers. |

Implementation rule for dhs: support every version of every spec we
claim, exposed as a selectable parameter. Full support means the
plugin can act as a **proxy gateway** that re-exposes one side to
the other (NMOS in, NMOS out — letting any controller drive any
device through dhs).

## Authoritative sources

| Resource | URL |
|---|---|
| AMWA homepage | https://www.amwa.tv/ |
| AMWA specifications index | https://www.amwa.tv/specifications |
| NMOS specs portal | https://specs.amwa.tv/nmos/ |

---

## 1. Resource Management

Spec index: https://specs.amwa.tv/nmos/#resource-management

Roles to support: **proxy gateway** + **consumer** + **provider**.

| ID | Name | Spec status | Release(s) |
|---|---|---|---|
| IS-04 | Discovery & Registration | AMWA Specification (Stable) | v1.3.3 / v1.2.2 / v1.1.3 |
| IS-09 | System Parameters | AMWA Specification | v1.0.0 |
| IS-13 | Annotation | Work In Progress | — |
| BCP-002-01 | Natural Grouping | AMWA Specification | v1.0.0 |
| BCP-002-02 | Asset Distinguishing Information | AMWA Specification | v1.0.0 |
| INFO-004 | Implementation Guide for DNS-SD | AMWA Specification | — |
| INFO-005 | Implementation Guide for NMOS Controllers | AMWA Specification | — |

---

## 2. Connection Management

Spec index: https://specs.amwa.tv/nmos/#connection-management

Roles to support: **proxy gateway** + **consumer** + **provider**.

| ID | Name | Spec status | Release(s) |
|---|---|---|---|
| IS-05 | Device Connection Management | AMWA Specification (Stable) | v1.1.2 / v1.0.2 |
| IS-08 | Audio Channel Mapping | AMWA Specification (Stable) | v1.0.1 |
| BCP-004-01 | Receiver Capabilities | AMWA Specification | v1.0.0 |
| BCP-004-02 | Sender Capabilities | AMWA Specification | v1.0.0 |
| BCP-006-01 | NMOS With JPEG XS | AMWA Specification | v1.0.0 |
| BCP-006-02 | NMOS With H.264 | Work In Progress | — |
| BCP-006-03 | NMOS With H.265 | Work In Progress | — |
| BCP-006-04 | NMOS Support for MPEG Transport Streams | AMWA Specification | v1.0.0 |
| BCP-007-01 | NMOS With NDI | Work In Progress | — |
| INFO-003 | Sink Metadata Processing Architecture | Work In Progress | — |
| INFO-005 | Implementation Guide for NMOS Controllers | AMWA Specification | — |

---

## 3. Device Control & Monitoring

Spec index: https://specs.amwa.tv/nmos/#device-control--monitoring

Roles to support: **proxy gateway** + **consumer** + **provider**.

| ID | Name | Spec status | Release(s) |
|---|---|---|---|
| IS-07 | Event & Tally | AMWA Specification | v1.0.1 |
| IS-12 | Control Protocol | AMWA Specification | v1.0.1 |
| MS-05-01 | NMOS Control Architecture | AMWA Specification | v1.0.0 |
| MS-05-02 | NMOS Control Framework | AMWA Specification | v1.0.0 |
| BCP-008-01 | NMOS Receiver Status Monitoring | AMWA Specification | v1.0.0 |
| BCP-008-02 | NMOS Sender Status Monitoring | AMWA Specification | v1.0.0 |
| INFO-006 | Implementation Guide for NMOS Device Control & Monitoring | AMWA Specification | — |
