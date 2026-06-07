# ACP1 Provider

> **Status: shipping**
>
> Serves a canonical tree.json as an AxonNet ACP1 device over UDP
> (Mode A per [../CLAUDE.md](../CLAUDE.md) "Transport modes"). Symmetric
> to the consumer plugin at [internal/acp1/consumer/](../../../internal/acp1/consumer/);
> reuses the same Message codec, value codec, and type constants.
>
> CLI: `dhs producer acp1 serve --tree <path> --host 0.0.0.0 --port 2071`.
> See [runbook.md](runbook.md) for the operator workflow.
