# ACP2 Provider

> **Status: shipping**
>
> Serves a canonical tree.json as an EVS Neuron / Axon Synapse ACP2
> device over AN2/TCP (port 2072, AN2 proto=2 per
> [../CLAUDE.md](../CLAUDE.md) "Transport"). Symmetric to the consumer
> at [internal/acp2/consumer/](../../../internal/acp2/consumer/);
> reuses the AN2 framer, ACP2 codec, and property codec verbatim.
>
> CLI: `dhs producer acp2 serve --tree <path> --host 0.0.0.0 --port 2072`.
> See [runbook.md](runbook.md) for the operator workflow.
