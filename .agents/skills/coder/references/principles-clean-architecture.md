# RULE TO WORK WITH THIS REPO
# Prinsip Desain dan Clean Architecture

Dokumen ini berisi diagram Mermaid ringkas untuk:

- KISS
- YAGNI
- PARETO
- SOLID
- SOC
- Folder structure
- Clean Architecture

## 1. Decision flow prinsip saat desain

```mermaid
flowchart TB
  A[Mulai dari goal dan konteks] --> B{Bisa dibatasi scope}
  B -->|Ya| C[Pareto focus pada core path]
  B -->|Tidak| C
  C --> D{Bisa dibuat lebih sederhana}
  D -->|Ya| E[KISS pilih opsi paling simple]
  D -->|Tidak| F
  E --> F{Ini kebutuhan sekarang}
  F -->|Tidak| G[YAGNI tunda fitur]
  F -->|Ya| H
  G --> H{Ada tanggung jawab bercampur}
  H -->|Ya| I[SoC pecah boundary]
  H -->|Tidak| J
  I --> J{Desain melanggar SOLID}
  J -->|Ya| K[Refactor minimal sesuai SOLID]
  J -->|Tidak| L[Implement]
  K --> L
```

## 2. Clean Architecture dan arah dependency

Diagram ini menekankan aturan utama: dependency mengarah ke dalam.

```mermaid
flowchart TB
  Fwk[Frameworks and Drivers] --> Adp[Interface Adapters]
  Adp --> UC[Use Cases]
  UC --> Ent[Entities]

  Note1[Dependency rule arah ke dalam] -.-> Adp
  Note1 -.-> UC
  Note1 -.-> Ent
```

## 3. SoC mapping ke layer

```mermaid
flowchart LR
  UI[UI atau Transport] --> Usecase[Use case orchestration]
  Usecase --> Domain[Domain rules]
  UI -.calls.-> Usecase
  Usecase -.reads writes.-> Infra[Infra state store]
```

## 4. Folder structure yang selaras dengan Clean Architecture

### 4.1 Struktur repo level tinggi

```mermaid
flowchart TB
  Repo[repo root] --> App[app]
  App --> Android[android-agent]
  App --> Server[server-agent]
  Repo --> Docs[docs dan plans]
  Docs --> Plans[plans]
```

### 4.2 android-agent sebagai execution edge

Mapping high-level ke layer Clean Architecture. Ini bukan kontrak wajib, hanya guideline agar boundary jelas.

```mermaid
flowchart TB
  AA[android-agent] --> Boot[boot]
  AA --> Service[service]
  AA --> Transport[transport]
  AA --> Observation[observation]
  AA --> Action[action]
  AA --> State[state]
  AA --> Agent[agent]

  Boot -.framework.-> FwkA[Frameworks]
  Service -.framework.-> FwkA
  Transport -.adapter.-> AdpA[Adapters]
  Observation -.adapter.-> AdpA
  Action -.entity model.-> EntA[Entities]
  Agent -.use case.-> UcA[Use Cases]
  State -.state management.-> UcA
```

### 4.3 server-agent sebagai control plane

Untuk server-agent yang belum diimplementasikan, struktur minimalis yang sering cukup:

```mermaid
flowchart TB
  SA[server-agent] --> Cmd[cmd]
  SA --> Internal[internal]
  Internal --> Domain[domain]
  Internal --> Usecases[usecases]
  Internal --> Adapters[adapters]
  Internal --> Infra[infra]
  Adapters --> Ws[transport ws]
  Infra --> Store[state store]
  Infra --> Queue[task queue]
```

## 5. SOLID ringkas sebagai checklist

```mermaid
flowchart TB
  SOLID[SOLID] --> S[Single responsibility]
  SOLID --> O[Open closed]
  SOLID --> L[Liskov substitution]
  SOLID --> I[Interface segregation]
  SOLID --> D[Dependency inversion]
```

