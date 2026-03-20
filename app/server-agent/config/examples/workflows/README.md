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
    kind: android.screen.changed
    package: com.android.settings
    text_contains: "Private DNS"

  action:
    kind: click
    target:
      kind: resource_id
      value: "com.android.settings:id/save_button"

  expect:
    kind: android.screen.changed
    text_contains: "Saved"

  timeout: 8s
  max_retry: 2
  on_success: next_step
  on_failure: terminal
```

Keterangan:
- `trigger`: event yang membuat step aktif. `trigger: {}` = auto-execute segera saat step dicapai.
- `action`: command ke android-agent (`open_app`, `open_intent`, `click`, `long_click`, `input_text`, `scroll`, `observe`, `fill_form`).
- `expect`: event konfirmasi hasil action. Jika tidak ada, step langsung advance ke `on_success`. Mendukung operator `or` (lihat bagian 5).
- `timeout` + `max_retry`: kontrol retry bila UI belum berubah.
- `on_success` / `on_failure`: transisi ke step berikutnya.

### Referensi `action.kind` (yang didukung saat ini)

`server-agent` saat ini menerima `action.kind` berikut:
`open_app`, `open_intent`, `click`, `long_click`, `input_text`, `scroll`, `observe`, `fill_form`.

| kind | Fungsi | Field minimal | Catatan |
|---|---|---|---|
| `open_app` | Buka aplikasi | `package` | Praktiknya gunakan `package` agar agent bisa resolve app tujuan. |
| `open_intent` | Buka intent Android | `intent_action` | `package` opsional (untuk membatasi intent ke package tertentu). |
| `click` | Tap/click elemen | `target` | `target` wajib (`kind` + `value`). |
| `long_click` | Long press elemen | `target` | `target` wajib (`kind` + `value`). |
| `input_text` | Isi teks pada field | `target` | `input_text` opsional, default string kosong bila tidak diisi. |
| `scroll` | Scroll kontainer atau layar aktif | - | `target` opsional. `direction` opsional (default forward). |
| `observe` | Ambil snapshot UI tanpa aksi | - | Digunakan untuk sinkronisasi state/diagnostik. |
| `fill_form` | Isi banyak field sekaligus | `fields` (minimal 1) | Tiap field wajib punya `target`; diproses dalam satu round-trip. |

Contoh singkat:

```yaml
# open_app
action:
  kind: open_app
  package: com.android.settings

# open_intent
action:
  kind: open_intent
  intent_action: "android.settings.ADD_ACCOUNT_SETTINGS"
  package: com.google.android.gsf

# scroll (target opsional)
action:
  kind: scroll
  direction: backward

# fill_form
action:
  kind: fill_form
  fields:
    - target:
        kind: resource_id
        value: "com.example:id/email"
      value: "{{input.email}}"
    - target:
        kind: resource_id
        value: "com.example:id/password"
      value: "{{input.password}}"
```

---

## 4) Event kinds untuk `trigger` dan `expect`

Semua field `trigger` / `expect` bersifat AND — hanya field yang diisi yang dievaluasi.

### `android.screen.changed` ⭐ paling umum

Dikirim setelah UI settle (debounce 250 ms). Otomatis di-dedup bila `semanticDigest` sama.

```yaml
trigger:
  kind: android.screen.changed
  package: com.android.settings          # nama package foreground (exact match)
  class_suffix: .NetworkDashboardFragment # suffix dari class name activity/fragment
  text_contains: "Private DNS"           # substring di teks visible / content-desc
  ui:
    active_ui_key: "settings.network"    # semantic key layar aktif
    base_screen_key: "settings.network"  # screen key tanpa overlay
    overlay_key: "dialog.confirm"        # key overlay / dialog (hanya saat overlay muncul)
    ui_ready: true                        # apakah semantic projection menganggap UI siap
    form_key: "form.primary"             # ada form dengan key ini
    button_key: "button.sign_in"         # ada button dengan key ini
    button_enabled: true                 # button tersebut enabled
    focused_target_key: "form.primary.email"  # field yang sedang focused
```

**Kapan pakai:** menunggu konten layar berubah, form muncul, teks tertentu tampil.

**Limitasi:** di-drop bila `semanticDigest` identik dengan event sebelumnya — gunakan
`android.activity.created` bila perlu mendeteksi transisi layar tanpa perubahan konten.

---

### `android.activity.created` — window transition tanpa dedup

Dikirim langsung saat `TYPE_WINDOW_STATE_CHANGED` — tanpa settle wait, tanpa dedup digest.
Cocok untuk mendeteksi activity/screen baru terbuka meski konten belum berubah.

```yaml
trigger:
  kind: android.activity.created
  package: com.android.settings          # package dari activity baru
  class_suffix: .PrivateDnsActivity      # suffix class name activity
```

**Payload fields yang dipakai matcher:** `packageName`, `className`.

**Kapan pakai:**
- Menunggu app tertentu masuk foreground setelah `open_app`
- Mendeteksi navigasi ke Settings sub-screen yang kontennya mirip layar sebelumnya
- `expect` setelah action navigasi ketika `android.screen.changed` mungkin di-dedup

```yaml
# Contoh: expect setelah open_app — lebih cepat dari screen.changed
open_settings:
  trigger: {}
  action:
    kind: open_app
    package: com.android.settings
  expect:
    kind: android.activity.created
    package: com.android.settings
  on_success: next_step
```

---

### `android.notification` — toast dan notifikasi sistem

Dikirim saat `TYPE_NOTIFICATION_STATE_CHANGED`. Tidak memerlukan UI settle.
Berguna untuk menangkap OTP via notifikasi SMS, konfirmasi toast, atau alert sistem.

```yaml
trigger:
  kind: android.notification
  package: com.android.mms              # app yang mengirim notifikasi
  text_contains: "kode verifikasi"      # substring di teks notifikasi
```

**Payload fields yang dipakai matcher:** `packageName`, `text[]`, `contentDescription`.

**Catatan:** `text_contains` dicocokkan ke semua string di array `text` dan `contentDescription`.

```yaml
# Contoh: trigger saat OTP masuk dari SMS
wait_otp:
  trigger:
    kind: android.notification
    text_contains: "kode verifikasi"
  action:
    kind: observe
  on_success: fill_otp
  on_failure: terminal
```

---

### `android.accessibility.disabled` — service dimatikan

Dikirim saat accessibility service di-interrupt atau di-disable (misalnya user mematikan manual).
Umumnya dipakai sebagai sinyal failure, bukan trigger workflow biasa.

```yaml
trigger:
  kind: android.accessibility.disabled
```

---

### `trigger: {}` — auto-execute (no wait)

Step langsung dieksekusi saat dicapai tanpa menunggu event apapun.

```yaml
step_name:
  trigger: {}
  action:
    kind: click
    target:
      kind: text
      value: "Simpan"
```

---

### Ringkasan perbandingan

| Kind | Settle wait | Dedup | Payload utama | Kapan pakai |
|---|---|---|---|---|
| `android.screen.changed` | ✅ 250 ms | ✅ semanticDigest | package, text, ui.* | konten UI berubah |
| `android.activity.created` | ❌ langsung | ❌ tidak | package, class | activity/screen baru terbuka |
| `android.notification` | ❌ langsung | ❌ tidak | package, text | notifikasi / toast |
| `android.accessibility.disabled` | ❌ | ❌ | — | service dimatikan |
| `trigger: {}` | — | — | — | auto-execute segera |

---

## 5) OR combinator pada `expect`

Gunakan `or` ketika konfirmasi bisa datang dari beberapa event path (misalnya screen change *atau* toast notification).

```yaml
save:
  trigger: {}
  action:
    kind: click
    target:
      kind: resource_id
      value: "com.android.settings:id/button1"
  expect:
    or:
      - kind: android.screen.changed
        text_contains: "Saved"
      - kind: android.notification
        text_contains: "Berhasil"
  on_success: terminal
  on_failure: terminal
```

**Aturan:**
- `or` membutuhkan minimal 2 clause.
- `or` **tidak bisa** digabung dengan field lain (`kind`, `package`, `text_contains`, `ui`) di level yang sama — pilih salah satu.
- Nested `or` (or di dalam clause or) tidak didukung.
- Setiap clause mengikuti semantik AND biasa (semua field dalam clause harus match).

---

## 6) Input artifact dari task

Gunakan placeholder `{{input.<key>}}` untuk membaca input saat task dibuat.

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

---

## 7) Rekomendasi selector biar stabil lintas device

Urutan prioritas:
1. `resource_id`
2. `content_description`
3. `text`
4. `class`

Hindari selector koordinat absolut untuk workflow umum lintas ukuran layar.

---

## 8) Contoh workflow Private DNS

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
      kind: android.activity.created
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
      kind: android.screen.changed
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
      kind: android.screen.changed
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
      kind: android.screen.changed
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
      kind: android.screen.changed
      package: com.android.settings
    on_success: terminal
    on_failure: terminal
```

---

## 9) Uji workflow via curl

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

---

## 10) Debug cepat saat workflow macet

- Cek `expect` terlalu ketat atau tidak match event aktual.
- Cek selector target masih valid di UI device saat ini.
- Tambah step `observe` di titik rawan untuk membantu transisi state.
- Kurangi ambiguitas text selector, utamakan `resource_id`.
- Jika `android.screen.changed` tidak muncul padahal layar berubah, coba ganti ke `android.activity.created` — kemungkinan `semanticDigest` identik sehingga event di-dedup.
- Jika butuh menunggu notifikasi/OTP sebelum lanjut, gunakan `android.notification` sebagai `trigger` di step terpisah.
