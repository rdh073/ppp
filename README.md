# PPP Monorepo Runbook

Panduan build sampai run untuk:
- `app/server-agent` (Go control plane)
- `app/android-agent` (Kotlin Android app)
- `app/dashboard` (React + Vite UI)

## Quick Start (5 menit)

Jalur cepat untuk local run dengan asumsi dependency sudah terpasang.

### 1) Start server-agent

```bash
cd app/server-agent
export AUTO_TOOL_LLM_API_KEY="sk-..."
go run ./cmd/server -config ./config/server.toml
```

### 2) Install dan arahkan android-agent ke server

```bash
cd app/android-agent
./gradlew installDebug
adb shell setprop auto.agent.server_url ws://<HOST_IP>:3000/ws/agent
```

Catatan:
- Emulator Android Studio biasanya pakai `ws://10.0.2.2:3000/ws/agent`
- Waydroid/device fisik pakai IP host LAN

### 3) Start dashboard

```bash
cd app/dashboard
npm install
export VITE_API_URL="http://localhost:3000"
export VITE_POLL_MS="5000"
npm run dev -- --host 0.0.0.0 --port 5173
```

Open `http://localhost:5173`.

### 4) Smoke check cepat

```bash
curl -sS http://localhost:3000/healthz
curl -sS http://localhost:3000/devices | jq
curl -sS http://localhost:3000/workflows | jq '.[].name'
```

### 5) Helper script (opsional)

```bash
# build semua komponen
./scripts/ppp_build_all.sh

# run server + dashboard
./scripts/ppp_run_server.sh
./scripts/ppp_run_dashboard.sh

# android install + set server url
./scripts/ppp_android_install.sh
./scripts/ppp_android_set_server_url.sh ws://10.0.2.2:3000/ws/agent

# smoke test
./scripts/ppp_smoke_test.sh

# create task contoh
./scripts/ppp_create_task_waydroid.sh <DEVICE_ID>
./scripts/ppp_create_task_private_dns.sh <DEVICE_ID> dns.quad9.net
```

### 6) Panduan bikin workflow

Lihat panduan lengkap authoring workflow YAML di:
- `app/server-agent/config/workflows/README.md`

---

## Full Setup (detail)

### 1. Prasyarat

- Linux/macOS dengan `bash`
- Go `1.23+`
- Node.js `20+` dan `npm`
- Java `17` (untuk Android Gradle)
- Android SDK + `adb`
- Waydroid atau Android emulator/device

Cek cepat:

```bash
go version
node -v
npm -v
java -version
adb version
```

### 2. Konfigurasi Server Agent

Sudah ada config nyata di:
- `app/server-agent/config/server.toml`

Set OpenAI key dari environment (disarankan), jangan hardcode di file:

```bash
export AUTO_TOOL_LLM_API_KEY="sk-..."
```

Jika sudah disimpan di `~/.bashrc`, reload shell:

```bash
source ~/.bashrc
```

### 3. Build Semua Komponen

#### 3.1 server-agent

```bash
cd app/server-agent
go mod tidy
go build -o bin/server-agent ./cmd/server
go test ./...
```

#### 3.2 android-agent

```bash
cd app/android-agent
./gradlew assembleDebug
./gradlew test
```

APK debug hasil build:
- `app/android-agent/app/build/outputs/apk/debug/app-debug.apk`

#### 3.3 dashboard

```bash
cd app/dashboard
npm install
npm run build
```

### 4. Run Semua Komponen

Disarankan urutan: `server-agent` → `android-agent` → `dashboard`.

#### 4.1 Jalankan server-agent

```bash
cd app/server-agent
go run ./cmd/server -config ./config/server.toml
```

Endpoint default:
- HTTP: `http://localhost:3000`
- WS agent: `ws://localhost:3000/ws/agent`
- Metrics: `http://localhost:3000/metrics`

#### 4.2 Jalankan android-agent (Waydroid/Emulator/Device)

Install APK:

```bash
cd app/android-agent
./gradlew installDebug
```

Set URL server pada device:

```bash
adb shell setprop auto.agent.server_url ws://<HOST_IP>:3000/ws/agent
```

Catatan:
- Untuk emulator Android Studio ke host lokal biasanya: `ws://10.0.2.2:3000/ws/agent`
- Untuk Waydroid/device fisik gunakan IP host LAN, bukan `localhost`

Aktifkan accessibility service agent di device, lalu buka app `android-agent` sekali agar service bootstrap.

#### 4.3 Jalankan dashboard

```bash
cd app/dashboard
export VITE_API_URL="http://localhost:3000"
export VITE_POLL_MS="5000"
npm run dev -- --host 0.0.0.0 --port 5173
```

Buka:
- `http://localhost:5173`

### 5. Smoke Test End-to-End

#### 5.1 Cek server health

```bash
curl -sS http://localhost:3000/healthz
```

#### 5.2 Cek device sudah register

```bash
curl -sS http://localhost:3000/devices | jq
```

#### 5.3 Cek workflow examples ter-load

```bash
curl -sS http://localhost:3000/workflows | jq '.[].name'
```

#### 5.4 Trigger task contoh (Waydroid workflow)

```bash
curl -sS -X POST http://localhost:3000/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "goal":"create google account on waydroid",
    "deviceId":"<DEVICE_ID>",
    "workflowName":"google-account-create-waydroid"
  }' | jq
```

Pantau status task:

```bash
curl -sS http://localhost:3000/tasks/<TASK_ID> | jq
```

#### 5.5 Trigger task contoh (Private DNS workflow)

```bash
curl -sS -X POST http://localhost:3000/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "goal":"set private dns",
    "deviceId":"<DEVICE_ID>",
    "workflowName":"android-settings-private-dns",
    "inputArtifacts":{
      "private_dns_hostname":"dns.quad9.net"
    }
  }' | jq
```

Pantau status task:

```bash
curl -sS http://localhost:3000/tasks/<TASK_ID> | jq
```

### 7. Troubleshooting Singkat

- `android-agent` tidak connect:
  - cek `adb devices`
  - cek `setprop auto.agent.server_url`
  - cek log server untuk `agent.hello` / `agent.resume`
- dashboard kosong:
  - pastikan `VITE_API_URL` mengarah ke server-agent yang benar
  - cek tab Network di browser (`/devices`, `/workflows`, `/events/accepted`)
- tool LLM gagal:
  - pastikan `AUTO_TOOL_LLM_API_KEY` ada di environment process `server-agent`
