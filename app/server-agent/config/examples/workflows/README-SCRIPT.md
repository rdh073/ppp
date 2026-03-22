# JS Script Bridge — `agent/script`

Scripts run inside [Mozilla Rhino 1.9.1](https://github.com/mozilla/rhino) in interpreted mode
(`optimizationLevel = -1`). The bridge registers the functions below as globals before the script
executes. All functions are synchronous. Errors throw into JS so scripts can use `try/catch`.

---

## Selectors

Most functions accept a **selector object** to identify a UI element:

```js
{ kind: "text",        value: "OK" }
{ kind: "resource_id", value: "com.android.settings:id/button1" }
{ kind: "semantic_key", value: "button.sign_in" }
{ kind: "content_desc", value: "Close" }
{ kind: "package_name", value: "com.android.settings" }
{ kind: "target_id",   value: "<opaque id from observe()>" }
{ kind: "coordinate",  value: "540,960" }
{ kind: "bounds",      value: "0,0,1080,200" }
```

---

## Functions

### `observe() → object`

Returns the current UI snapshot as a plain JS object. Shape mirrors the `UiSnapshot` wire format:
`snapshotId`, `deviceId`, `packageName`, `activityName`, `screenState`, `semantic`, `targets`, etc.

```js
var snap = observe();
log(snap.semantic.activeUiKey);
var texts = snap.targets.map(function(t) { return t.text; });
```

Throws if the accessibility tree is unavailable.

---

### `tap(action) → { ok: true }`

Dispatches a device action. The `action` object shape:

| `kind`       | Extra fields | Description |
|---|---|---|
| `"click"`    | `target` (selector) | Tap the matching element |
| `"long_press"` | `target` (selector) | Long-press the matching element |
| `"input_text"` | `target` (selector), `inputText` (string) | Type text into a field |
| `"open_app"` | `target: { kind: "package_name", value: "..." }` | Launch app by package |
| `"open_intent"` | `intentAction` (string), `package?` (string) | Launch via Android Intent |
| `"back"`     | — | Press Back |
| `"home"`     | — | Press Home |

```js
tap({ kind: "click", target: { kind: "text", value: "Sign in" } });
tap({ kind: "open_app", target: { kind: "package_name", value: "com.android.settings" } });
tap({ kind: "open_intent", intentAction: "android.settings.PRIVATE_DNS_SETTINGS" });
tap({ kind: "open_intent", intentAction: "android.intent.action.VIEW", package: "com.android.chrome" });
```

Throws on action failure (element not found, not actionable, etc.).

---

### `input(selector, text) → { ok: true }`

Shorthand for typing text into a field identified by `selector`.

```js
input({ kind: "resource_id", value: "com.example:id/email" }, "user@example.com");
```

Equivalent to `tap({ kind: "input_text", target: selector, inputText: text })`.
Throws on failure.

---

### `waitFor(selector, timeoutMs?) → boolean`

Polls the accessibility tree every 200 ms until `selector` matches a target or `timeoutMs`
elapses. Returns `true` if found, `false` on timeout. Default timeout: 5 000 ms.

```js
if (!waitFor({ kind: "text", value: "Private DNS" }, 5000)) {
  throw new Error("Private DNS option not visible");
}
```

Does **not** throw on timeout — check the return value.

---

### `scroll(selector?, direction?) → { ok: true }`

Scrolls the view identified by `selector`. If `selector` is omitted or `null`, scrolls the
currently focused scrollable container.

`direction`: `"forward"` (default, scrolls down/right) or `"backward"` (scrolls up/left).

```js
scroll({ kind: "resource_id", value: "com.android.settings:id/list" }, "forward");
scroll(null, "backward");
```

Throws on failure.

---

### `awaitEvent(kind, opts?, timeoutMs?) → boolean`

Blocks until a matching Android accessibility event fires. Returns `true` if received within the
timeout, `false` otherwise. Does **not** throw on timeout.

| `kind` | Android event |
|---|---|
| `"activity_created"` / `"window_state_changed"` | `TYPE_WINDOW_STATE_CHANGED` — new activity or dialog |
| `"content_changed"` | `TYPE_WINDOW_CONTENT_CHANGED` — content updated within the current window |

`opts` (optional):
- `package` — filter by app package name
- `textContains` — filter; event must include this substring in its text payload

```js
// Tap Next, then wait for new activity before observing
tap({ kind: "click", target: { kind: "text", value: "Next" } });
awaitEvent("activity_created", { package: "com.google.android.gms" }, 5000);
var snap = observe();

// Wait for content change with no filter
awaitEvent("content_changed", {}, 3000);
```

**Timing note:** the watcher is registered when `awaitEvent` is called. If the transition already
fired before `awaitEvent` runs, the call waits for the *next* matching event and returns `false`
after the timeout. Pair with `waitFor` for robustness:

```js
tap({ kind: "click", target: { kind: "text", value: "Next" } });
awaitEvent("activity_created", {}, 4000);          // fast path
waitFor({ kind: "text", value: "Birthday" }, 4000); // safety net
```

---

### `back() → { ok: true }`

Presses the system Back button. Throws on failure.

```js
back();
```

---

### `home() → { ok: true }`

Presses the system Home button. Throws on failure.

```js
home();
```

---

### `log(...args) → undefined`

Appends a message to the script's log list. Arguments are joined with a space. Logs are
returned in the `device.script` response alongside `output` and `durationMs`.

```js
log("navigating to", params.section);
log("found target:", snap.semantic.activeUiKey);
```

---

### `screenshot() → { base64: string, mimeType: "image/png" }`

Captures the current screen as a PNG, returned as base64. Requires Android API 30 (Android 11+).
Throws if the device is below API 30 or the capture callback fails.

```js
var shot = screenshot();
// shot.base64 — PNG encoded as base64, no newlines (Base64.NO_WRAP)
// shot.mimeType — always "image/png"
```

Typical use: pass to a server-side vision endpoint for captcha solving or visual verification.

---

### `http(opts) → { status: number, body: string }`

Makes a synchronous HTTP request from the device. Useful for fetching data mid-script.

| Field | Type | Required | Default |
|---|---|---|---|
| `url` | string | yes | — |
| `method` | string | no | `"GET"` |
| `body` | string | no | none |
| `headers` | object | no | none |

```js
var res = http({
  url: "https://api.example.com/token",
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ device: params.deviceId }),
});
if (res.status !== 200) throw new Error("token fetch failed: " + res.status);
var token = JSON.parse(res.body).token;
```

The request runs synchronously on the calling thread. Throws if the TCP connection fails.

---

### Captcha solving via `screenshot()` + `http()`

The server exposes `POST /captcha/solve` backed by a vision LLM. Pass the screenshot base64
and receive tap coordinates. Requires `AUTO_TOOL_ANTHROPIC_API_KEY` + `AUTO_TOOL_ANTHROPIC_MODEL`
on the server; returns HTTP 503 when unconfigured.

```js
// 1. Detect captcha presence
var found = waitFor({ kind: "text", value: "Select all" }, 3000);
if (!found) return { captchaSolved: false, skipped: true };

// 2. Capture screen
var shot = screenshot();

// 3. Ask server to solve
var resp = http({
  url: params.captchaEndpoint,   // e.g. "http://10.0.2.2:3000/captcha/solve"
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ imageBase64: shot.base64 })
});
if (resp.status !== 200) throw new Error("captcha solve error: " + resp.status);

// 4. Tap each coordinate
var result = JSON.parse(resp.body);  // { taps: ["x,y", ...], count: N }
for (var i = 0; i < result.taps.length; i++) {
  tap({ kind: "click", target: { kind: "coordinate", value: result.taps[i] } });
}
return { captchaSolved: true, count: result.count };
```

Request body: `{ imageBase64, gridBounds?: [l,t,r,b], cols?: N }` — `gridBounds` and `cols`
override the LLM's auto-detected values when you already know the grid layout.

---

## Prototyping — direct device endpoints

Two HTTP endpoints let you iterate on scripts **without writing a YAML workflow or creating a task**.

### `POST /devices/{id}/observe`

Returns the current UI snapshot from the device immediately.

```bash
curl -X POST http://localhost:3000/devices/emulator-5554/observe | jq .semantic.activeUiKey
```

Useful for checking what is on screen before writing a script.

---

### `POST /devices/{id}/script`

Runs a JS snippet directly on the device and returns the output synchronously.

| Field | Type | Required | Default |
|---|---|---|---|
| `source` | string | yes | — |
| `params` | object | no | `{}` |
| `timeout` | number (ms) | no | `30000` |

```bash
curl -X POST http://localhost:3000/devices/emulator-5554/script \
  -H "Content-Type: application/json" \
  -d '{
    "source": "var s = observe(); log(s.packageName); return { pkg: s.packageName };",
    "timeout": 10000
  }'
```

Response:

```json
{ "output": { "pkg": "com.android.settings" }, "logs": ["com.android.settings"], "durationMs": 123 }
```

The `params` object is available inside the script as the `params` global — same as in a workflow step.

```bash
curl -X POST http://localhost:3000/devices/emulator-5554/script \
  -H "Content-Type: application/json" \
  -d '{
    "source": "tap({ kind: \"open_intent\", intentAction: \"android.settings.PRIVATE_DNS_SETTINGS\" }); return { ok: waitFor({ kind: \"text\", value: \"Private DNS\" }, 5000) };",
    "timeout": 15000
  }'
```

---

## Script contract

- Scripts are wrapped in an IIFE; `return` at the top level is valid.
- The `params` global is a plain object populated from the workflow step's `params` map
  (after `{{input.key}}` interpolation).
- The script's return value must be a plain object (`{}`). Primitives and arrays are ignored —
  the workflow engine only maps top-level keys from the return object.
- Timeout is enforced via Rhino's instruction observer (every 10 000 instructions). The default
  is 30 s; override with `timeout:` in the `script:` step definition.

```js
// Minimal well-formed script
var hostname = params.hostname;
tap({ kind: "open_app", target: { kind: "package_name", value: "com.android.settings" } });
waitFor({ kind: "text", value: "Private DNS" }, 5000);
return { configured: true };
```
