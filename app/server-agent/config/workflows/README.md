# Workflow Authoring Guide

Panduan membuat workflow YAML untuk `server-agent`. **Pattern yang direkomendasikan: script** (RhinoJS via `device.script`). Lihat [README-SCRIPT.md](README-SCRIPT.md) untuk referensi bridge functions lengkap.

## 1) Jalankan server dengan live reload workflow

```bash
cd app/server-agent
go run ./cmd/server -workflow-dir ./config/workflows
```

Perubahan file workflow akan direload otomatis (default polling 5 detik).

## 2) Struktur workflow script (direkomendasikan)

```yaml
name: my-workflow
version: 1
entry: start

steps:
  start:
    trigger: {}
    script:
      source: |
        tap({ kind: "open_app", target: { kind: "package_name", value: "com.example.app" } });
        if (!waitFor({ kind: "text", value: "Welcome" }, 5000)) {
          throw new Error("app did not open");
        }
        return { done: true };
      params:
        someParam: "{{input.some_input}}"
      outputs:
        done: task_done
      timeout: 30s
    on_success: terminal
    on_failure: terminal
```

Keterangan:
- `trigger: {}` — auto-execute segera saat step dicapai.
- `script.source` — kode JavaScript (ES6, Rhino). `params` dan semua bridge functions tersedia sebagai globals.
- `script.params` — map key→value; value mendukung `{{input.key}}` interpolation.
- `script.outputs` — memetakan key dari return object ke workflow artifacts.
- `script.timeout` — batas waktu eksekusi script (default 30s).

## 3) Contoh workflow yang tersedia

| File | Fungsi | Input artifacts |
|---|---|---|
| `android-settings-private-dns-script.yaml` | Set Private DNS di Settings | `private_dns_hostname` |
| `captcha-script.yaml` | Solve image CAPTCHA via vision LLM | `captcha_endpoint` |
| `instagram-login-script.yaml` | Login Instagram | `username`, `password` |

## 4) Bridge functions ringkas

| Fungsi | Signature | Keterangan |
|---|---|---|
| `observe()` | `→ UiSnapshot` | Snapshot UI saat ini |
| `tap(action)` | `→ {ok}` | Dispatch action (click, open_app, back, home, dll) |
| `input(selector, text)` | `→ {ok}` | Input teks ke field |
| `waitFor(selector, ms?)` | `→ boolean` | Poll UI sampai selector match / timeout |
| `scroll(selector?, dir?)` | `→ {ok}` | Scroll container (`forward`/`backward`) |
| `back()` | `→ {ok}` | Tombol Back |
| `home()` | `→ {ok}` | Tombol Home |
| `screenshot()` | `→ {base64, mimeType}` | Capture layar (API 30+) |
| `http(opts)` | `→ {status, body}` | HTTP request sinkron dari device |
| `log(...args)` | `→ undefined` | Tulis ke script log |

Lihat [README-SCRIPT.md](README-SCRIPT.md) untuk dokumentasi lengkap per-function.

## 5) Input artifact dari task

Gunakan `{{input.<key>}}` di `script.params` untuk meneruskan input task ke script:

```yaml
script:
  params:
    hostname: "{{input.private_dns_hostname}}"
```

Saat create task:

```json
{
  "workflowName": "android-settings-private-dns-script",
  "inputArtifacts": {
    "private_dns_hostname": "dns.quad9.net"
  }
}
```

## 6) Uji workflow via curl

```bash
# list workflow yang ter-load
curl -sS http://127.0.0.1:3000/workflows | jq '.[].name'

# buat task
curl -sS -X POST http://127.0.0.1:3000/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "goal": "set private dns",
    "deviceId": "<DEVICE_ID>",
    "workflowName": "android-settings-private-dns-script",
    "inputArtifacts": { "private_dns_hostname": "dns.quad9.net" }
  }' | jq

# cek status task
curl -sS http://127.0.0.1:3000/tasks/<TASK_ID> | jq
```

## 7) Debug saat workflow macet

- Tambah `log(observe().semantic.activeUiKey)` untuk melihat UI key saat ini.
- Cek return value `waitFor` — returns `false` bila timeout, tidak throw.
- Naikkan `timeout:` bila script butuh lebih dari 30s.
- Gunakan `screenshot()` + log untuk inspeksi visual state layar.
