# SYNC cancellation responsibility

> Status: locked

## Shared model inputs

- [Request](../../architecture/data-model/orders.schema.json#/$defs/request)
- [Response](../../architecture/data-model/orders.schema.json#/$defs/response)

## Declared obligations

| 本合同条款 | Promise | Frontend responsibility | Backend responsibility |
|:---|:---|:---|:---|
| SYNC-001 §1 | [Cancel order](#cancel-order): shared request/response, terminal outcomes and rejection side effects. | FE-001 §1 | BE-001 §1 |

<a id="cancel-order"></a>
## Cancel order — §1

POST /cancel consumes the request and returns the response. An open order becomes
cancelled (200). An already completed order returns completed (409) and is not
mutated. The client must not report successful cancellation for a 409 response.
An invalid request is rejected before business state is touched (400).

[Return to the contract index](CONTRACTS-001.md#shared-model-baseline).
