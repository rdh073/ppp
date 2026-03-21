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
| `"back"`     | — | Press Back |
| `"home"`     | — | Press Home |

```js
tap({ kind: "click", target: { kind: "text", value: "Sign in" } });
tap({ kind: "open_app", target: { kind: "package_name", value: "com.android.settings" } });
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
