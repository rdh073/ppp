# Example HTTP Tool Provider Catalog

This directory is a minimal catalog slice for the shipped example `http` provider path.

Use it for:

- catalog-loader and remote-tool integration tests
- validating the `GET /v1/tools` and `POST /v1/tools/{name}:invoke` contract
- demonstrating how to add a non-builtin provider without changing central registry wiring

It is not a drop-in replacement for [`config/tools`](/home/xtrzy/Workspace/ppp/app/server-agent/config/tools). It only contains the example remote provider manifest and binding.

Run the example provider:

```bash
cd app/server-agent
go run ./cmd/tool-provider-example
```

Exercise the example catalog:

```bash
cd app/server-agent
AUTO_TOOL_EXAMPLE_BASE_URL=http://127.0.0.1:3310 \
go test ./internal/tools -run HTTPProvider
```

Start a dedicated server instance against this example catalog:

```bash
cd app/server-agent
AUTO_TOOL_EXAMPLE_BASE_URL=http://127.0.0.1:3310 \
go run ./cmd/server -tool-dir ./config/examples/http-provider
```
