# Probel SW-P-02 Provider

> **Status: shipping**
>
> Serves a canonical tree.json as an SW-P-02 matrix controller (tx side)
> over TCP (default port 2002, per [../CLAUDE.md](../CLAUDE.md) "Transport").
> Symmetric to the consumer plugin at
> [internal/probel-sw02p/consumer/](../../../internal/probel-sw02p/consumer/);
> reuses the same byte codec (`internal/probel-sw02p/codec/`) for the
> SOM/COMMAND/MESSAGE/CHECKSUM framer and every per-command codec.
>
> One listener accepts many client sessions; each session reads framed
> commands and the dispatcher answers interrogate / connect / status /
> protect / salvo / router-config requests, fanning out tx 04 CONNECTED
> and tx 03 TALLY to every connected session per §3.
>
> CLI: `dhs producer probel-sw02p serve --tree <path> --host 0.0.0.0 --port 2002`.
> See [runbook.md](runbook.md) for the operator workflow.
