# Model upstream Responses probe

[简体中文](../model-upstream-probe.md)

An administrator with `MCPUpstreamUpdate` permission can call `POST /api/v1/admin/model-proxy/upstream-accounts/test` to select a model already configured on the account:

```json
{"id":"ACCOUNT_ID","model":"RESPONSES_MODEL_ID"}
```

Leading and trailing whitespace is removed from `model`. It must belong to the account's current merged catalog, including synchronized and manually configured models. An unconfigured model returns HTTP 400 without an upstream request. Omitting `model`, or providing a whitespace-only value, preserves the previous behavior of using the first catalog entry; this does not establish that the first entry supports Responses. The existing console button keeps this compatibility behavior.

The endpoint sends a streaming Responses request using only the selected model and returns the actual probe `model`, `reply`, and `latency_ms`. Success requires text and completion events; errors and incomplete streams remain failures. It does not modify manual models, the catalog, or model-group routing, and it does not automatically try other models.

Offline regressions use a local HTTP test server and an in-memory store. They cover a mixed catalog whose first entry does not support Responses, explicit selection, rejection of an unconfigured model, and an unchanged catalog. They do not read real provider credentials or use a production database. Live model/protocol verification and private credential delivery require a separately agreed scope.
