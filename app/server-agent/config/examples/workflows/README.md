# Workflow YAML Examples

Declarative `WorkflowDef` documents for the `server-agent` workflow engine.

## Loading

```bash
# Serve with live-reload from this directory
cd app/server-agent
go run ./cmd/server -workflow-dir ./config/examples/workflows
```

Workflows are hot-reloaded every 5 s (configurable with `-workflow-poll`).

## StepDef schema

```yaml
name: my-workflow
version: 2
entry: first_step   # step id to start from

steps:
  first_step:
    # trigger: which device event activates this step.
    # Empty trigger ({}) matches any event.
    # Empty trigger + action/tool_call → auto-executed immediately when reached.
    trigger:
      kind: android.window.state_changed   # EventKind filter
      package: com.android.settings        # packageName exact match
      class_suffix: PrivateDnsSettings     # className suffix match
      text_contains: "Private DNS"         # any text field substring

    # action: typed device command (mutually exclusive with tool_call).
    action:
      kind: click          # open_app | click | long_click | input_text | scroll | observe
      target:
        kind: text         # text | resource_id | content_description | class
        value: "Save"      # supports {{input.key}} interpolation
      input_text: "..."    # for input_text kind
      package: "com.foo"   # for open_app kind
      direction: down      # for scroll kind: up | down | left | right

    # tool_call: invoke a registered catalog tool (mutually exclusive with action).
    tool_call:
      tool_name: identity.generate_indonesian_name
      params:
        gender: "{{input.gender}}"        # interpolated from state.Inputs
      outputs:
        fullName: username                # result JSON key → state.Inputs key
      optional: false                     # if true, failure is skipped (OnSuccess path)

    # expect: event that confirms the action succeeded.
    # Nil = advance immediately after action.
    expect:
      kind: android.window.state_changed
      text_contains: "{{input.private_dns_hostname}}"

    timeout: 10s      # how long to wait for expect (default 10s)
    max_retry: 3      # retries before following on_failure
    on_success: next_step
    on_failure: terminal
```

## Step execution model

| Trigger | Action/ToolCall | Behaviour |
|---------|----------------|-----------|
| Non-empty | any | Wait for matching device event, then execute |
| Empty | Action or ToolCall | Auto-execute immediately when reached via `advance()` |
| Empty | None (routing only) | Wait for any device event |

**"UI no transition" handling:** every action step arms an `expect` with a `timeout`.
If the UI doesn't respond before the deadline, the next incoming event triggers a retry
(`handleFailure`). After `max_retry` the step follows `on_failure`.

## Available tool names (local catalog)

| Tool | Params | Outputs |
|------|--------|---------|
| `identity.generate_indonesian_name` | `gender` (male\|female) | `fullName`, `firstName`, `lastName` |
| `identity.generate_email` | `fullName`, `domain` | `email` |
| `identity.generate_birth_date` | `minAge`, `maxAge`, `referenceDate` | `birthDate` |
| `credential.generate_password` | `length`, `includeSymbols` | `password` |

Model-backed and HTTP tools are loaded from `config/tools/` — see that directory's README.

## Private DNS example

```bash
curl -X POST http://127.0.0.1:3000/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "goal": "set private dns",
    "deviceId": "dev-123",
    "workflowName": "android-settings-private-dns",
    "inputArtifacts": {
      "private_dns_hostname": "dns.example.com"
    }
  }'
```

The workflow opens Settings, navigates to Private DNS, enters the hostname, and saves.
Each step auto-executes immediately after the previous Expect event fires, so the full
sequence runs with no extra events from the operator.
