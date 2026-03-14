# Workflow YAML Examples

These files are example `WorkflowDef` documents for the `server-agent` workflow API.

Use them in either mode:

```bash
cd app/server-agent
go run ./cmd/server -workflow-dir ./config/examples/workflows
```

Or upload one directly:

```bash
curl -X PUT \
  -H 'Content-Type: application/yaml' \
  --data-binary @./config/examples/workflows/android-settings-private-dns.yaml \
  http://127.0.0.1:3000/workflows/android-settings-private-dns
```

Create a task against the built-in planner path with:

```bash
curl -X POST \
  -H 'Content-Type: application/json' \
  -d '{
    "goal": "set private dns",
    "deviceId": "dev-123",
    "workflowName": "android-settings-private-dns",
    "inputArtifacts": {
      "private_dns_hostname": "dns.example.com"
    }
  }' \
  http://127.0.0.1:3000/tasks
```

Current state:

- The repo now ships a built-in planner path for workflow name `android-settings-private-dns`.
- The workflow still depends on one required input artifact: `private_dns_hostname`.
- The planner uses heuristic Settings labels and scroll detection, so OEM-specific Settings layouts can still require additional variants.

Action payloads referenced by the example follow the existing `device.execute` JSON contract, for example:

```json
{"action":{"kind":"open_app","target":{"kind":"package_name","value":"com.android.settings"}}}
```

```json
{"action":{"kind":"click","target":{"kind":"text","value":"Private DNS"}}}
```

```json
{"action":{"kind":"input_text","target":{"kind":"resource_id","value":"android:id/edit"},"inputText":"dns.example.com"}}
```
