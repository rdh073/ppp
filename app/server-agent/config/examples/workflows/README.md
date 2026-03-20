# Workflow Authoring Guide

Panduan membuat workflow YAML untuk `server-agent`.

## 1) Jalankan server dengan live reload workflow

```bash
cd app/server-agent
go run ./cmd/server -workflow-dir ./config/examples/workflows
```

Perubahan file workflow akan direload otomatis (default polling 5 detik).

## 2) Struktur minimum workflow

```yaml
name: my-workflow
version: 2
entry: start

steps:
  start:
    trigger: {}
    action:
      kind: observe
    on_success: terminal
```

## 3) Struktur step yang umum dipakai

```yaml
some_step:
  trigger:
    kind: android.window.state_changed
    package: com.android.settings
    text_contains: "Private DNS"

  action:
    kind: click
    target:
      kind: resource_id
      value: "com.android.settings:id/save_button"

  expect:
    kind: android.window.state_changed
    text_contains: "Saved"

  timeout: 8s
  max_retry: 2
  on_success: next_step
  on_failure: terminal
```

Keterangan:
- `trigger`: event yang membuat step aktif.
- `action`: command ke android-agent (`open_app`, `click`, `long_click`, `input_text`, `scroll`, `observe`).
- `expect`: event konfirmasi hasil action.
- `timeout` + `max_retry`: kontrol retry bila UI belum berubah.
- `on_success` / `on_failure`: transisi ke step berikutnya.

## 4) Input artifact dari task

Gunakan placeholder `{{input.<key>}}` untuk membaca input saat task dibuat.

Contoh:

```yaml
action:
  kind: input_text
  target:
    kind: resource_id
    value: "com.android.settings:id/edittext"
  input_text: "{{input.private_dns_hostname}}"
```

Saat create task:

```json
{
  "workflowName": "android-settings-private-dns",
  "inputArtifacts": {
    "private_dns_hostname": "dns.quad9.net"
  }
}
```

## 5) Rekomendasi selector biar stabil lintas device

Urutan prioritas:
1. `resource_id`
2. `content_description`
3. `text`
4. `class`

Hindari selector koordinat absolut untuk workflow umum lintas ukuran layar.

## 6) Contoh workflow Private DNS

```yaml
name: android-settings-private-dns
version: 2
entry: open_settings

steps:
  open_settings:
    trigger: {}
    action:
      kind: open_app
      package: com.android.settings
    expect:
      kind: android.window.state_changed
      package: com.android.settings
    on_success: open_network
    on_failure: terminal

  open_network:
    trigger: {}
    action:
      kind: click
      target:
        kind: text
        value: "Network & internet"
    expect:
      kind: android.window.state_changed
      package: com.android.settings
    on_success: open_private_dns
    on_failure: terminal

  open_private_dns:
    trigger: {}
    action:
      kind: click
      target:
        kind: text
        value: "Private DNS"
    expect:
      kind: android.window.state_changed
      text_contains: "Private DNS"
    on_success: fill_private_dns
    on_failure: terminal

  fill_private_dns:
    trigger: {}
    action:
      kind: input_text
      target:
        kind: resource_id
        value: "com.android.settings:id/private_dns_mode_hostname"
      input_text: "{{input.private_dns_hostname}}"
    expect:
      kind: android.window.state_changed
      text_contains: "{{input.private_dns_hostname}}"
    on_success: save
    on_failure: terminal

  save:
    trigger: {}
    action:
      kind: click
      target:
        kind: resource_id
        value: "com.android.settings:id/button1"
    expect:
      kind: android.window.state_changed
      package: com.android.settings
    on_success: terminal
    on_failure: terminal
```

## 7) Uji workflow via curl

```bash
# list workflow yang ter-load
curl -sS http://127.0.0.1:3000/workflows | jq '.[].name'

# buat task
curl -sS -X POST http://127.0.0.1:3000/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "goal":"set private dns",
    "deviceId":"<DEVICE_ID>",
    "workflowName":"android-settings-private-dns",
    "inputArtifacts":{"private_dns_hostname":"dns.quad9.net"}
  }' | jq

# cek status task
curl -sS http://127.0.0.1:3000/tasks/<TASK_ID> | jq
```

## 8) Debug cepat saat workflow macet

- Cek `expect` terlalu ketat atau tidak match event aktual.
- Cek selector target masih valid di UI device saat ini.
- Tambah step `observe` di titik rawan untuk membantu transisi state.
- Kurangi ambiguitas text selector, utamakan `resource_id`.
