# Real LLM Provider Examples

These examples target the current model-tool runtime in [`internal/tools/llm.go`](/home/xtrzy/Workspace/ppp/app/server-agent/internal/tools/llm.go).

They do not change the default catalog. They only provide provider-specific environment examples for the shipped `content.generate_welcome_email` prompt tool in [`config/tools`](/home/xtrzy/Workspace/ppp/app/server-agent/config/tools).

## Usage

Run from `app/server-agent`:

```bash
set -a
source ./config/examples/llm-providers/openai.env.example
set +a
go run ./cmd/server
```

Swap `openai.env.example` for `anthropic.env.example`, `gemini.env.example`, or `deepseek.env.example` as needed.

## Notes

- The current runtime expects an OpenAI-style chat-completions endpoint in `AUTO_TOOL_LLM_API_URL`.
- The current runtime sends `response_format.type=json_schema` and validates the returned JSON again after the provider responds.
- OpenAI, Gemini, and DeepSeek examples map directly onto the current runtime contract.
- Anthropic is included through its OpenAI compatibility endpoint, but provider-specific behavior may still differ; keep workflow fallback enabled for model-backed steps.

## Files

- [`openai.env.example`](/home/xtrzy/Workspace/ppp/app/server-agent/config/examples/llm-providers/openai.env.example)
- [`anthropic.env.example`](/home/xtrzy/Workspace/ppp/app/server-agent/config/examples/llm-providers/anthropic.env.example)
- [`gemini.env.example`](/home/xtrzy/Workspace/ppp/app/server-agent/config/examples/llm-providers/gemini.env.example)
- [`deepseek.env.example`](/home/xtrzy/Workspace/ppp/app/server-agent/config/examples/llm-providers/deepseek.env.example)
