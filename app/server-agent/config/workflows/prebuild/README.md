# Workflow Prebuild — Account Manager

Folder ini berisi workflow YAML yang digunakan secara langsung oleh sistem **Account Manager** PPP.

## ⚠️ Perhatian

**Jangan mengubah file di folder ini sembarangan.**

Workflow di sini dipanggil secara otomatis oleh kode server-agent (Go) dengan nama yang sudah terdaftar. Mengubah nama workflow, menghapus langkah penting, atau mengubah nama input yang diharapkan **akan menyebabkan Account Manager gagal berjalan** tanpa pesan error yang jelas.

Jika perlu melakukan perubahan, koordinasikan dengan tim yang bertanggung jawab atas `internal/accountmanager/`.

---

## Daftar Workflow

| File | Nama Workflow | Kegunaan |
|------|---------------|----------|
| `google-account-login-script.yaml` | `google-account-login-script` | Login akun Google di perangkat Android |
| `google-account-create-script.yaml` | `google-account-create-script` | Buat akun Google secara manual (data persona disuplai dari luar) |
| `google-account-create-auto-script.yaml` | `google-account-create-auto-script` | Buat akun Google otomatis (persona di-generate oleh LLM) |
| `instagram-account-login-script.yaml` | `instagram-account-login-script` | Login akun Instagram (dengan penanganan prompt pasca-login) |
| `instagram-login-script.yaml` | `instagram-login-script` | Login Instagram sederhana |
| `instagram-create-script.yaml` | `instagram-create-script` | Buat akun Instagram baru dengan verifikasi OTP |
| `instagram-post-script.yaml` | `instagram-post-script` | Upload gambar dan caption ke feed Instagram |
| `captcha-script.yaml` | `captcha-script` | Selesaikan tantangan CAPTCHA via endpoint eksternal |

---

## Input Wajib per Workflow

### `google-account-login-script`
- `email` — alamat Gmail
- `password` — kata sandi

### `google-account-create-script`
- `first_name`, `last_name`, `gender`
- `birth_day`, `birth_month`, `birth_year`
- `username`, `password`
- `phone_number`, `captcha_endpoint`, `account_service_endpoint`

### `google-account-create-auto-script`
- `phone_number`, `captcha_endpoint`, `account_service_endpoint`, `device_id`, `phone_otp`
- `gender` (opsional)

### `instagram-account-login-script` / `instagram-login-script`
- `email` / `username`, `password`

### `instagram-create-script`
- `phone_number`, `account_service_endpoint`, `device_id`, `phone_otp`
- `first_name`, `last_name`, `username`, `password`
- `google_account_id` (opsional)

### `instagram-post-script`
- `caption`, `imagePath`

### `captcha-script`
- `captcha_endpoint`

---

## Cara Kerja Loading

File-file di folder ini dimuat otomatis oleh server-agent saat startup melalui `FSDefStore`, yang sekarang mendukung rekursi ke subdirektori. Cukup pastikan server dijalankan dengan flag `--workflow-dir` mengarah ke folder induk (`config/workflows/`).

```bash
go run ./cmd/server --workflow-dir ./config/workflows
```

Semua file `.yaml` di `config/workflows/` **dan** `config/workflows/prebuild/` akan dimuat sekaligus.

## Limitation

Harus di tuning ulang sesuaikan UI device yang digunakan