# Tool Providers: LLM Setup Guide

Panduan setup LLM untuk semua provider yang tersedia di:
- `app/server-agent/config/tools/providers.yaml`

## Provider yang tersedia

### LLM / Vision providers
- `openai-native` (`kind: openai`)
- `deepseek-native` (`kind: deepseek`)
- `anthropic-native` (`kind: anthropic`)
- `gemini-native` (`kind: gemini`)
- `anthropic-vision-native` (`kind: anthropic-vision`)

### Non-LLM providers
- `builtin` (`kind: builtin`)
- `example-http` (`kind: http`)
- `account-http` (`kind: http`)

## Aturan enable/disable provider

- Semua provider `optional: true` akan tetap boot walau belum dikonfigurasi.
- Jika belum lengkap, provider akan di-skip dan muncul warning:
  - `optional model provider disabled ... reason="model not configured"`
  - `optional vision provider disabled ... reason="model not configured"`
  - `optional tool provider disabled ... reason="baseURL not configured"`

## Matrix env per provider

| Provider ID | Kind | API URL env | API Key env | Model env | Env tambahan |
|---|---|---|---|---|---|
| `openai-native` | `openai` | `AUTO_TOOL_OPENAI_API_URL` | `AUTO_TOOL_OPENAI_API_KEY` | `AUTO_TOOL_OPENAI_MODEL` | - |
| `deepseek-native` | `deepseek` | `AUTO_TOOL_DEEPSEEK_API_URL` | `AUTO_TOOL_DEEPSEEK_API_KEY` | `AUTO_TOOL_DEEPSEEK_MODEL` | - |
| `anthropic-native` | `anthropic` | `AUTO_TOOL_ANTHROPIC_API_URL` | `AUTO_TOOL_ANTHROPIC_API_KEY` | `AUTO_TOOL_ANTHROPIC_MODEL` | `AUTO_TOOL_ANTHROPIC_API_VERSION` |
| `gemini-native` | `gemini` | `AUTO_TOOL_GEMINI_API_URL` | `AUTO_TOOL_GEMINI_API_KEY` | `AUTO_TOOL_GEMINI_MODEL` | - |
| `anthropic-vision-native` | `anthropic-vision` | `AUTO_TOOL_ANTHROPIC_API_URL` | `AUTO_TOOL_ANTHROPIC_API_KEY` | `AUTO_TOOL_ANTHROPIC_MODEL` | `AUTO_TOOL_ANTHROPIC_API_VERSION` |

## Default endpoint (kalau API URL env tidak diisi)

- OpenAI: `https://api.openai.com/v1/chat/completions`
- DeepSeek: `https://api.deepseek.com/chat/completions`
- Anthropic: `https://api.anthropic.com/v1/messages`
- Gemini: `https://generativelanguage.googleapis.com/v1beta`
- Anthropic Vision: `https://api.anthropic.com/v1/messages`

## Contoh `.bashrc` (pilih provider yang ingin dipakai)

```bash
# OpenAI
export AUTO_TOOL_OPENAI_API_KEY="sk-..."
export AUTO_TOOL_OPENAI_MODEL="gpt-4o-mini"

# DeepSeek
# export AUTO_TOOL_DEEPSEEK_API_KEY="..."
# export AUTO_TOOL_DEEPSEEK_MODEL="deepseek-chat"

# Anthropic
# export AUTO_TOOL_ANTHROPIC_API_KEY="..."
# export AUTO_TOOL_ANTHROPIC_MODEL="claude-3-5-sonnet-latest"
# export AUTO_TOOL_ANTHROPIC_API_VERSION="2023-06-01"

# Gemini
# export AUTO_TOOL_GEMINI_API_KEY="..."
# export AUTO_TOOL_GEMINI_MODEL="gemini-1.5-pro"
```

Reload shell:

```bash
source ~/.bashrc
```

## Jalankan server dengan tool catalog

```bash
cd app/server-agent
go run ./cmd/server -config ./config/server.toml
```

## Verifikasi provider aktif dari log

Cek log startup:

```bash
cd app/server-agent
go run ./cmd/server -config ./config/server.toml 2>&1 | rg "optional .* provider disabled|runtime recovery complete|server-agent starting"
```

Jika env sudah benar, warning untuk provider terkait tidak muncul lagi.

## Catatan kompatibilitas

- Tool manifest bisa punya `providers` chain (fallback urutan provider).
- Jika provider pertama unavailable, engine akan lanjut ke provider berikutnya di chain.
- `builtin` tetap tersedia tanpa API key.

## Legacy generic LLM env (opsional)

Selain native providers di `providers.yaml`, server juga masih mendukung env generik:
- `AUTO_TOOL_LLM_API_URL`
- `AUTO_TOOL_LLM_API_KEY`
- `AUTO_TOOL_LLM_MODEL`

Ini dipakai oleh jalur OpenAI-compatible generik (bukan per-provider native).
