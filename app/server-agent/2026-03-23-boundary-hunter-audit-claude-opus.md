# Boundary Hunter Audit — 2026-03-23

## Scope

- Surface: codebase (`app/server-agent/`)
- Files: 179 Go files across 26 packages
- Exclusions: `vendor/`, test files (for consumer analysis)

## Package Map

| Package | Exported Symbols | External Deps | Fan-In | Fan-Out |
| ------- | ---------------- | ------------- | ------ | ------- |
| `domain` | ~30 types, 5 fns | 1 (`yaml.v3`) | 14 | 0 internal |
| `registry` | 3 types, 2 fns | 0 | 6 | 1 (domain) |
| `store` | ~20 types/interfaces, 8 fns | 3 (pgx, redis, sql) | 8 | 2 (domain, projection) |
| `workflow` | ~15 types, 8 fns | 1 (yaml.v3) | 3 | 1 (domain) |
| `dispatcher` | 2 types, 1 fn | 0 | 2 | 3 (domain, registry, store, telemetry) |
| `orchestrator` | 2 types, 1 fn | 0 | 1 | 3 (domain, store, workflow, telemetry) |
| `handler` | ~10 types | 1 (yaml.v3) | 1 | 8 |
| `devicectrl` | ~15 types | 2 (gadb, websocket) | 3 | 4 (domain, registry, store, **tools/llm**) |
| `appport` | 4 interfaces | 0 | 5 | 5 (devicectrl, eventing, registry, workflowruntime, domain) |
| `tools` | ~16 types/fns | 0 | 2 | 1 (tools/llm) |
| `tools/llm` | ~20 types/fns | 0 | 4 | 0 internal |
| `tools/loader` | 3 fns | 2 (yaml.v3, template) | 1 | 3 (tools, tools/llm, tools/vision) |
| `workflowruntime` | 5 types, 3 fns | 0 | 4 | 3 (domain, registry, store) |
| `eventing` | 5 types, 2 fns | 0 | 3 | 3 (domain, store, telemetry) |
| `eventruntime` | 2 types | 1 (redis) | 1 | 3 (domain, store, telemetry) |
| `telemetry` | ~10 types | 0 | 4 | 1 (domain) |
| `projection` | 4 types | 0 | 3 | 0 internal |
| `transport/ws` | 1 fn | 1 (websocket) | 1 | 5 |
| `infra/llm` | 2 interfaces | 0 | 0 | 0 internal |
| `accountmanager` | 1 type | 0 | 1 | 4 |
| `campaigns` | 1 type | 0 | 1 | 4 |

## Dependency Graph Issues

### Direction Violations

#### V1. Domain imports external serialization library (HIGH)

`internal/domain/workflowdef.go:7` imports `gopkg.in/yaml.v3`.

The domain layer should be pure — no external deps. The `StringOrJSONMap.UnmarshalYAML(*yaml.Node)` method at line 107 hard-couples domain to yaml.v3.

**Action:** Move `UnmarshalYAML` to a domain adapter/codec in `workflow` or a dedicated `domain/codec` package. Domain type keeps the data, serialization stays outside.

#### V2. appport (port package) imports concrete implementations (HIGH)

`internal/appport/port.go:7-11` imports:
- `devicectrl` — concrete types: `HelloRequest`, `HelloResponse`, `ResumeRequest`, `ResumeResponse`
- `workflowruntime` — concrete types: `CreateTaskRequest`, `ListTaskQuery`, `TaskSummary`
- `eventing` — concrete types: `AcceptedEventQuery`, `AcceptedEventPage`, `DeadLetterQuery`, `DeadLetterPage`, `AcceptedPayloadPreview`
- `registry` — concrete type: `AgentSummary`

Lines 60-63 contain compile-time interface satisfaction checks against concrete types.

This is **inverted dependency**: the port package depends on its implementors instead of the other way around. Consumers of `appport` are transitively coupled to all implementation packages.

**Action:** Move request/response types into `appport` itself (or `domain`). Move compile-time checks to `cmd/server/main.go` (composition root).

#### V3. devicectrl imports tools/llm (MEDIUM)

`internal/devicectrl/http_device.go:16` and `internal/devicectrl/llm_recorder.go:15` import `tools/llm`.

Symbols used: `llm.AgentLoop`, `llm.AgentTool`, `llm.AgentLoopRequest`, `llm.ErrAgentDone`.

Device control should not know about LLM tooling. These are orthogonal concerns: device lifecycle vs AI model orchestration.

**Action:** Define a local `RecordingLoop` interface in `devicectrl` that mirrors the needed contract. Wire the concrete `AgentLoop` at the composition root.

#### V4. accountmanager and campaigns import workflowruntime (MEDIUM)

- `internal/accountmanager/deps.go:6` — aliases `workflowruntime.CreateTaskRequest`, uses `workflowruntime.PollTaskUntilTerminal`
- `internal/campaigns/deps.go:8` — same pattern

Feature modules directly depend on a concrete runtime implementation instead of going through `appport.TaskControl`.

**Action:** Move `CreateTaskRequest` to `appport` or `domain`. Replace direct `PollTaskUntilTerminal` calls with a port-level abstraction.

#### V5. store imports projection (INFO)

`internal/store/projection_events.go:10` imports `projection` for `projection.Event`, `projection.ErrInvalidCursor`, `projection.ErrBackfillUnavailable`.

This is a lateral dependency — store implements a projection store. Not a strict upward violation, but it couples store to projection semantics.

## Export Surface Issues

### Over-Exported API

#### `internal/tools` — 16 dead exports

The entire `llm_compat.go` file (10 symbols) is a dead compatibility shim. No external consumer uses these re-exports:

| # | Package | Export | Type | Ext. Consumers |
| - | ------- | ------ | ---- | -------------- |
| 1 | `tools` | `ModelToolConfig` | type alias | 0 |
| 2 | `tools` | `JSONModelClient` | type alias | 0 |
| 3 | `tools` | `JSONModelRequest` | type alias | 0 |
| 4 | `tools` | `VisionModelClient` | type alias | 0 |
| 5 | `tools` | `VisionAnalyzeRequest` | type alias | 0 |
| 6 | `tools` | `NewOpenAICompatibleJSONClient` | func wrapper | 0 |
| 7 | `tools` | `NewAnthropicMessagesJSONClient` | func wrapper | 0 |
| 8 | `tools` | `NewGeminiGenerateContentJSONClient` | func wrapper | 0 |
| 9 | `tools` | `NewDeepSeekChatCompletionsJSONClient` | func wrapper | 0 |
| 10 | `tools` | `NewAnthropicVisionClient` | func wrapper | 0 |
| 11 | `tools` | `NewDefaultToolRegistry` | func | 0 |
| 12 | `tools` | `NewModelToolRegistry` | func | 0 |
| 13 | `tools` | `ModelToolDefinitions` | func | 0 |
| 14 | `tools` | `NewCompositeToolRegistry` | func | 0 |
| 15 | `tools` | `IsToolRetryable` | func | 0 |
| 16 | `tools/llm` | `AgentLoopResult` | type | 0 (structurally reachable via return value) |

#### `internal/workflow` — 9 dead exports + 5 test-only

Never referenced by name outside the package:

| # | Package | Export | Type | Notes |
| - | ------- | ------ | ---- | ----- |
| 1 | `workflow` | `Node` | interface | internal engine dispatch only |
| 2 | `workflow` | `NodeCommand` | type | passed internally |
| 3 | `workflow` | `NodeOutput` | type | returned internally |
| 4 | `workflow` | `ActionNode` | type | never constructed externally |
| 5 | `workflow` | `ToolCallNode` | type | never constructed externally |
| 6 | `workflow` | `ScriptNode` | type | never constructed externally |
| 7 | `workflow` | `ActionDispatcher` | interface | satisfied structurally |
| 8 | `workflow` | `ToolInvoker` | interface | satisfied structurally |
| 9 | `workflow` | `EngineResult` | type | returned by `Engine.Handle` but never named externally |

Test-only exports (used only from `*_test.go` outside the package):

| # | Export | File |
| - | ------ | ---- |
| 1 | `Interpolate` | `interpolate.go:11` |
| 2 | `InterpolateStrict` | `interpolate.go:24` |
| 3 | `MatchEvent` | `matcher.go:34` |
| 4 | `MatchExpect` | `matcher.go:42` |
| 5 | `SnapshotMatchesExpect` | `snapshot_matcher.go:18` |

#### `internal/devicectrl` — 11 dead exports

All interfaces/types consumed only within the package:

| # | Export | Type | File |
| - | ------ | ---- | ---- |
| 1 | `AccessibilityAutoEnabler` | interface | `accessibility_auto_enabler.go:20` |
| 2 | `AccessibilityRemediationRequest` | type | `device_binding_manager.go:17` |
| 3 | `AccessibilityRemediationResult` | type | `device_binding_manager.go:23` |
| 4 | `AccessibilityRemediator` | interface | `device_binding_manager.go:32` |
| 5 | `DeviceBindingCoordinator` | interface | `device_binding_manager.go:42` |
| 6 | `AgentConnectedNotifier` | interface | `agent_lifecycle.go:17` |
| 7 | `EventProcessor` | interface | `agent_lifecycle.go:24` |
| 8 | `PendingTaskAssigner` | interface | `agent_lifecycle.go:28` |
| 9 | `RecordedEntry` | type | `recording.go:21` |
| 10 | `DefaultAccessibilityReconcileInterval` | const | `device_binding_manager.go:15` |
| 11 | `DefaultAccessibilityServiceComponent` | const | `accessibility_auto_enabler.go:15` |

#### `internal/handler` — 1 dead export

| # | Export | Type | File |
| - | ------ | ---- | ---- |
| 1 | `WorkflowReloader` | interface | `recording_library.go:22` |

### External Type Leaks

| # | Package | Export / Signature | External Type | Layer | Action |
| - | ------- | ------------------ | ------------- | ----- | ------ |
| 1 | `domain` | `StringOrJSONMap.UnmarshalYAML(*yaml.Node)` | `gopkg.in/yaml.v3` | domain | Move to codec adapter |

### Missing internal/ Packages

No findings — all implementation packages are already under `internal/`.

### Orphaned Package

| # | Package | Issue |
| - | ------- | ----- |
| 1 | `internal/infra/llm` | 0 internal fan-in. Defines `JSONModelClient` and `VisionModelClient` interfaces but no package imports it. |

## Consumer Access Violations

### Deep Imports

No violations. Sub-packages under `tools/` are imported only by siblings or parent, which is the intended structure.

## Replaceability Assessment

### `domain`

- Replaceable from API? **Almost.** The `yaml.v3` dependency in `UnmarshalYAML` is the only obstacle.
- Implicit contracts: `NewTaskID()` / `NewEventID()` use `crypto/rand`; uniqueness is assumed, not enforced.
- Coupling risk: **Low** (single external dep).

### `workflow`

- Replaceable from API? **Yes.** Only exports `Engine`, `NewEngine`, `FSDefStore`, and related types consumed by `orchestrator` and `handler`.
- Implicit contracts: Step graph traversal order, retry semantics.
- Coupling risk: **Low.**

### `appport`

- Replaceable from API? **No.** It re-exports types from `devicectrl`, `workflowruntime`, `eventing`. Replacing any implementation requires changing `appport` signatures.
- Coupling risk: **High.** This is the central anti-pattern — the port package is coupled to its implementors.

### `devicectrl`

- Replaceable from API? **Partially.** The `tools/llm` dependency means replacing device control also requires the LLM agent loop interface.
- Coupling risk: **Medium.**

### `tools/llm`

- Replaceable from API? **Yes.** Clean interface (`AgentLoop`), factory function, no upward deps.
- Coupling risk: **Low.**

## Recommendations (Priority Order)

### Must-fix

1. **Invert `appport` dependencies.** Move request/response types (`HelloRequest`, `CreateTaskRequest`, `ListTaskQuery`, etc.) into `appport` or `domain`. Implementation packages should import from `appport`, not the reverse. Move compile-time satisfaction checks to `cmd/server/main.go`.

2. **Remove `yaml.v3` from `domain`.** Extract `StringOrJSONMap.UnmarshalYAML` to a codec in `workflow` or `store` layer. Domain should have zero external deps.

### Should-fix

3. **Delete `internal/tools/llm_compat.go`** — entire file is dead code (10 type aliases + function wrappers with 0 consumers).

4. **Delete dead exports in `internal/tools/llm.go`** — `NewDefaultToolRegistry`, `NewModelToolRegistry`, `ModelToolDefinitions`, `NewCompositeToolRegistry`.

5. **Unexport `workflow` internals** — `Node`, `NodeCommand`, `NodeOutput`, `ActionNode`, `ToolCallNode`, `ScriptNode`, `ActionDispatcher`, `ToolInvoker`, `EngineResult`. These are implementation details of the engine.

6. **Unexport `devicectrl` internals** — 11 interfaces/types that serve no external consumer.

7. **Decouple `devicectrl` from `tools/llm`** — define a local `RecordingLoop` interface; inject the concrete `AgentLoop` at the composition root.

8. **Decouple `accountmanager`/`campaigns` from `workflowruntime`** — use `appport.TaskControl` instead of direct `workflowruntime` imports.

### Consider

9. **Investigate `internal/infra/llm`** — orphaned package with 0 consumers. Delete if superseded by `tools/llm`.

10. **Move test-only exports** (`Interpolate`, `MatchEvent`, etc.) behind `_test.go` export tests or an `internal/workflowtest` package.

11. **Evaluate `store` → `projection` coupling** — if projection types grow, consider moving shared types to domain.
