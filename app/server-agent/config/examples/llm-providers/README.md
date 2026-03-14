# Real LLM Provider Examples

These examples target the model-tool runtime in [`internal/tools/llm.go`](/home/xtrzy/Workspace/ppp/app/server-agent/internal/tools/llm.go).

There are now two example paths:

- OpenAI-compatible env examples for the default built-in prompt tool path in [`config/tools`](/home/xtrzy/Workspace/ppp/app/server-agent/config/tools)
- Native provider catalogs for OpenAI, Anthropic, Gemini, and DeepSeek under `catalogs/`

## Usage

Run from `app/server-agent`:

```bash
set -a
source ./config/examples/llm-providers/openai.env.example
set +a
go run ./cmd/server
```

Swap `openai.env.example` for `anthropic.env.example`, `gemini.env.example`, or `deepseek.env.example` as needed when you want the OpenAI-compatible path.

For the native provider catalogs:

```bash
cd app/server-agent
AUTO_TOOL_OPENAI_API_KEY=... \
AUTO_TOOL_OPENAI_MODEL=gpt-4.1 \
go run ./cmd/server -tool-dir ./config/examples/llm-providers/catalogs/openai
```

```bash
cd app/server-agent
AUTO_TOOL_ANTHROPIC_API_KEY=... \
AUTO_TOOL_ANTHROPIC_MODEL=... \
go run ./cmd/server -tool-dir ./config/examples/llm-providers/catalogs/anthropic
```

```bash
cd app/server-agent
AUTO_TOOL_GEMINI_API_KEY=... \
AUTO_TOOL_GEMINI_MODEL=gemini-2.5-flash \
go run ./cmd/server -tool-dir ./config/examples/llm-providers/catalogs/gemini
```

```bash
cd app/server-agent
AUTO_TOOL_DEEPSEEK_API_KEY=... \
AUTO_TOOL_DEEPSEEK_MODEL=deepseek-chat \
go run ./cmd/server -tool-dir ./config/examples/llm-providers/catalogs/deepseek
```

## Notes

- The default built-in prompt path still expects an OpenAI-style chat-completions endpoint in `AUTO_TOOL_LLM_API_URL`.
- The default built-in prompt path sends `response_format.type=json_schema` and validates the returned JSON again after the provider responds.
- OpenAI and DeepSeek still fit the built-in OpenAI-compatible path.
- OpenAI, Anthropic, Gemini, and DeepSeek now also have native provider kinds with loadable example catalogs.
- Keep workflow fallback enabled for model-backed steps even when using native providers.

## Files

- [`openai.env.example`](/home/xtrzy/Workspace/ppp/app/server-agent/config/examples/llm-providers/openai.env.example)
- [`anthropic.env.example`](/home/xtrzy/Workspace/ppp/app/server-agent/config/examples/llm-providers/anthropic.env.example)
- [`gemini.env.example`](/home/xtrzy/Workspace/ppp/app/server-agent/config/examples/llm-providers/gemini.env.example)
- [`deepseek.env.example`](/home/xtrzy/Workspace/ppp/app/server-agent/config/examples/llm-providers/deepseek.env.example)
- [`catalogs/openai`](/home/xtrzy/Workspace/ppp/app/server-agent/config/examples/llm-providers/catalogs/openai)
- [`catalogs/anthropic`](/home/xtrzy/Workspace/ppp/app/server-agent/config/examples/llm-providers/catalogs/anthropic)
- [`catalogs/gemini`](/home/xtrzy/Workspace/ppp/app/server-agent/config/examples/llm-providers/catalogs/gemini)
- [`catalogs/deepseek`](/home/xtrzy/Workspace/ppp/app/server-agent/config/examples/llm-providers/catalogs/deepseek)
