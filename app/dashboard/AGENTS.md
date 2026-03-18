# Dashboard

Kontrol UI operator untuk sistem automation (android-agent + server-agent).  
Lingkup implementasi: antarmuka web monitoring + intervensi manual yang memakai `server-agent` sebagai backend.

## Status Saat Ini

- Direktori `app/dashboard/` saat ini berisi spesifikasi (belum ada implementasi runtime).
- Fitur dashboard yang dideskripsikan berikut ini mengikuti **kontrak server-agent yang ada sekarang**, lalu bisa diperluas bertahap.

## Arsitektur

```text
Dashboard (React) --> REST API --> server-agent (Go, :3000) --> android-agents (/ws/agent)
```

## Tech Stack yang Dianjurkan

- React 18 + TypeScript
- Vite
- Zustand
- Tailwind CSS + komponen custom
- Testing: Vitest + React Testing Library
- Fetch API untuk HTTP; WebSocket hanya dipakai jika operator endpoint ditambahkan di backend

## API Server-Agent yang Benar (yang Ada Sekarang)

Semua endpoint di bawah berjalan pada root server (`VITE_API_URL`, default `http://localhost:3000`):

- `GET /healthz`
- `GET /openapi.json`
- `GET /swagger`

### Device

- `GET /devices` - daftar device yang sedang terdaftar di registry
- `GET /devices/{deviceId}` - detail device + session + capabilities

### Tasks

- `POST /tasks` - buat task
  - body: `goal` (wajib), `deviceId` (opsional), `workflowName` (opsional), `inputArtifacts` (opsional)
- `GET /tasks/{id}` - detail task
- `DELETE /tasks/{id}` - cancel task

### Workflows

- `GET /workflows` - daftar workflow definitions
- `GET /workflows/{name}` - detail workflow
- `PUT /workflows/{name}` - simpan / replace definition

### Events & Observability

- `GET /events/accepted` - daftar event yang diterima
- `GET /events/accepted/{eventId}` - detail event diterima
- `POST /events/accepted/{eventId}/replay` - replay event
- `GET /events/deadletters` - daftar dead letter
- `GET /events/deadletters/{id}` - detail dead letter
- `POST /events/deadletters/{id}/replay` - replay dead letter
- `GET /metrics` - metrics Prometheus

### Catatan Integrasi

- Tidak ada endpoint dashboard dengan prefix `/api`.
- Tidak ada `/tasks` list endpoint saat ini; untuk view operasional, dashboard perlu menampilkan task dari data task individual yang tersedia (`/tasks/{id}`) dan/atau event stream.
- Tidak ada endpoint snapshot dedicated (`/api/snapshots/:agentId`); snapshot muncul lewat event Android di `events` bila backend menyiapkannya.
- WebSocket operator `ws://.../ws/operator` belum ada. Satu-satunya WebSocket sekarang adalah `/ws/agent` untuk komunikasi android-agent.

## Fungsi Inti Dashboard (MVP)

1. **Device Registry**
   - Lihat list device online/offline dan sesi
   - Sort/filter by status/lastHeartbeat

2. **Task Control**
   - Buat task baru (`POST /tasks`)
   - Lihat status/detail task (`GET /tasks/{id}`)
   - Cancel task (`DELETE /tasks/{id}`)

3. **Workflow**
   - Tampilkan daftar workflow (`GET /workflows`)
   - Lihat detail workflow (`GET /workflows/{name}`)

4. **Events / Logs**
   - Poll `GET /events/accepted` untuk feed operasional
   - Filter by `deviceId`, `kind`, `source`, waktu, order, limit/pagination
   - Detail event (`GET /events/accepted/{eventId}`)
   - Replay event (`POST /events/accepted/{eventId}/replay`) untuk eskalasi

5. **Observability**
   - Buka `/metrics` untuk health/latency/throughput operator check

## Struktur Source yang Direkomendasikan

```text
app/dashboard/
 - src/
   - api/
     - client.ts
     - devices.ts
     - tasks.ts
     - workflows.ts
     - events.ts
   - components/
     - layout/
     - agents/
     - tasks/
     - workflows/
     - events/
     - logs/
     - controls/
   - hooks/
     - useDevices.ts
     - useTask.ts
     - useWorkflows.ts
     - useEvents.ts
   - store/
     - devices.ts
     - task.ts
     - workflow.ts
     - event.ts
   - types/
     - index.ts
   - App.tsx
   - main.tsx
 - package.json
 - vite.config.ts
 - tailwind.config.ts
 - tsconfig.json
```

## Pola Implementasi (Kecil, Realistis)

### 1) Baseline

- Setup proyek Vite + React + TS
- Buat HTTP client terpusat (`api/client.ts`) dengan error handling dan timeout
- Implement pages: Devices, Tasks, Workflows, Events

### 2) State dan Integrasi

- Zustand untuk cache ringan:
  - `devices`, `tasks`, `workflows`, `events`
- Polling interval:
  - Devices: 3-5 detik
  - Events: 5-10 detik
- Gunakan `Cursor`/`offset` params untuk paginasi events saat tersedia

### 3) Kontrol & Debug

- Form create task, tombol cancel task
- Event detail dan replay flow
- Export events JSON (optional)

## Konvensi Kode

- Functional component + hooks
- TypeScript strict, hindari `any` (kecuali boundary parsing dengan `unknown`)
- Semua API call melalui `api/*`, tidak scattering URL di UI
- Komponen UI harus graceful error states (loading, empty, error)

## Build & Development

```bash
# dari app/dashboard/
npm install
npm run dev
npm run build
npm run test
npm run lint
```

### Environment

```bash
VITE_API_URL=http://localhost:3000
VITE_POLL_MS=5000
VITE_METRICS_URL=http://localhost:3000/metrics
```

## Referensi

- `C4-Documentation/c4-code-scripts-dashboard.md`
- `app/server-agent/internal/handler/assets/openapi.json`
- `app/server-agent/cmd/server/main.go`
- `app/server-agent/internal/handler/openapi.go`
