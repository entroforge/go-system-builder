# Shared-model pilot (synthetic)

This isolated fixture demonstrates the first JSON Schema adapter. It is not a
product requirement or proof that a real frontend/backend product was tested.

From the factory repository:

```sh
go run ./cmd/loop-harness contracts check --root docs/examples/shared-model/project --json
go test ./internal/sharedmodel -run TestConsumerProviderPilot -v
```

Start with [the contract index](project/docs/dev/contracts/CONTRACTS-001.md), then
read [the shared operation](project/docs/dev/contracts/SYNC-001.md#cancel-order).
Both consumer contracts reference the same request and response schema.
The Go HTTP pilot uses that source to validate client requests, provider input,
responses and a mock. It exercises a successful cancellation, a structurally
invalid request, and a structurally valid request rejected for business state.
Changing `order_id` to a number or inventing a response state must fail before
integration is accepted. The test also checks that a business rejection does not
mutate provider state.

The fixture intentionally uses a standalone schema: the adapter does not parse
OpenAPI operations or generate TypeScript/Go clients. Target projects must wire
their own actual consumers and run their real integration tests.
