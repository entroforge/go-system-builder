# Cancellation contracts

> Status: locked
> Shared model policy: json-schema-v1

## Shared model baseline

| Operation | Slot | Schema | Consumers | Valid example | Structural negative |
|:---|:---|:---|:---|:---|:---|
| cancelOrder | request | [request](../../architecture/data-model/orders.schema.json#/$defs/request) | [FE](FE-001.md) [BE](BE-001.md) [SYNC](SYNC-001.md) | [valid](../../architecture/data-model/request.json) | [structural negative](../../architecture/data-model/invalid-request.json) |
| cancelOrder | response-200 | [response](../../architecture/data-model/orders.schema.json#/$defs/response) | [FE](FE-001.md) [BE](BE-001.md) [SYNC](SYNC-001.md) | [valid](../../architecture/data-model/response.json) | N/A |

## Coverage

FE-001 §1 · BE-001 §1 · SYNC-001 §1
