package com.autosdk.agent.agent.script

import com.autosdk.agent.action.ActionResult
import com.autosdk.agent.action.AutomationAction
import com.autosdk.agent.action.ScrollDirection
import com.autosdk.agent.action.Selector
import com.autosdk.agent.action.SelectorKind
import com.autosdk.agent.agent.AgentAutomationDriver
import com.autosdk.agent.agent.UiSnapshotSerializer
import com.autosdk.agent.observation.UiSnapshot
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import org.mozilla.javascript.BaseFunction
import org.mozilla.javascript.Context
import org.mozilla.javascript.NativeObject
import org.mozilla.javascript.Scriptable
import org.mozilla.javascript.ScriptableObject

/**
 * Registers synchronous automation functions onto a Rhino [Scriptable] scope.
 *
 * Each function bridges the JS script to the on-device [AgentAutomationDriver].
 * Errors are surfaced as Rhino EcmaErrors so scripts can catch them with try/catch.
 *
 * Call [register] once after [Context.initStandardObjects].
 */
class JsAutomationBridge(
    private val snapshotBuilder: () -> UiSnapshot?,
    private val automationDriver: AgentAutomationDriver,
    private val okHttpClient: OkHttpClient,
    val logs: MutableList<String> = mutableListOf(),
    private val screenshotCapture: () -> String? = { null },
    /** Blocks until an accessibility event matching [kind] fires or timeout elapses.
     *  kind: "activity_created" | "window_state_changed" | "content_changed"
     *  Returns true if the event was received, false on timeout. */
    private val eventAwaiter: (kind: String, pkg: String?, textContains: String?, timeoutMs: Long) -> Boolean = { _, _, _, _ -> false },
) {

    /** Registers all bridge functions into [scope]. */
    fun register(cx: Context, scope: Scriptable) {
        ScriptableObject.putProperty(scope, "observe", observeFn(cx, scope))
        ScriptableObject.putProperty(scope, "tap", tapFn(cx, scope))
        ScriptableObject.putProperty(scope, "input", inputFn(cx, scope))
        ScriptableObject.putProperty(scope, "waitFor", waitForFn(cx, scope))
        ScriptableObject.putProperty(scope, "scroll", scrollFn(cx, scope))
        ScriptableObject.putProperty(scope, "back", backFn(cx, scope))
        ScriptableObject.putProperty(scope, "home", homeFn(cx, scope))
        ScriptableObject.putProperty(scope, "log", logFn(cx, scope))
        ScriptableObject.putProperty(scope, "http", httpFn(cx, scope))
        ScriptableObject.putProperty(scope, "screenshot", screenshotFn(cx, scope))
        ScriptableObject.putProperty(scope, "awaitEvent", awaitEventFn(cx, scope))
    }

    // ---- bridge functions ----

    private fun observeFn(cx: Context, scope: Scriptable) = makeFn(scope, "observe") { _, _, _, _ ->
        val snapshot: UiSnapshot = snapshotBuilder()
            ?: scriptError("observe: accessibility tree not available")
        val json = UiSnapshotSerializer.toJson(snapshot).toString()
        cx.evaluateString(scope, "($json)", "<observe>", 1, null)
    }

    private fun tapFn(cx: Context, scope: Scriptable) = makeFn(scope, "tap") { _, _, args, _ ->
        val actionObj: NativeObject = args.getOrNull(0) as? NativeObject
            ?: scriptError("tap: expected action object")
        val action: AutomationAction = parseActionFromNative(actionObj)
            ?: scriptError("tap: invalid or unrecognised action object")
        val result = automationDriver.execute(action)
        if (result is ActionResult.Failed) {
            scriptError("tap: ${result.reason}")
        }
        buildResultObject(cx, scope, "ok" to true)
    }

    private fun inputFn(cx: Context, scope: Scriptable) = makeFn(scope, "input") { _, _, args, _ ->
        val selectorObj: NativeObject = args.getOrNull(0) as? NativeObject
            ?: scriptError("input: expected selector object")
        val text = args.getOrNull(1)?.toString() ?: ""
        val selector: Selector = parseSelectorFromNative(selectorObj)
            ?: scriptError("input: invalid selector")
        val result = automationDriver.execute(AutomationAction.InputText(selector, text))
        if (result is ActionResult.Failed) {
            scriptError("input: ${result.reason}")
        }
        buildResultObject(cx, scope, "ok" to true)
    }

    private fun waitForFn(cx: Context, scope: Scriptable) = makeFn(scope, "waitFor") { _, _, args, _ ->
        val selectorObj: NativeObject = args.getOrNull(0) as? NativeObject
            ?: scriptError("waitFor: expected selector object")
        val timeoutMs = (args.getOrNull(1) as? Number)?.toLong() ?: 5_000L
        val selector: Selector = parseSelectorFromNative(selectorObj)
            ?: scriptError("waitFor: invalid selector")

        val deadline = System.currentTimeMillis() + timeoutMs
        var found = false
        while (System.currentTimeMillis() < deadline) {
            val snapshot = snapshotBuilder()
            if (snapshot != null && snapshotContains(snapshot, selector)) {
                found = true
                break
            }
            Thread.sleep(200)
        }
        found
    }

    private fun scrollFn(cx: Context, scope: Scriptable) = makeFn(scope, "scroll") { _, _, args, _ ->
        val selectorObj = args.getOrNull(0) as? NativeObject
        val direction = args.getOrNull(1)?.toString() ?: "forward"
        val selector = selectorObj?.let { parseSelectorFromNative(it) }
        val dir = if (direction == "backward") ScrollDirection.BACKWARD else ScrollDirection.FORWARD
        val result = automationDriver.execute(AutomationAction.Scroll(selector, dir))
        if (result is ActionResult.Failed) {
            scriptError("scroll: ${result.reason}")
        }
        buildResultObject(cx, scope, "ok" to true)
    }

    private fun backFn(cx: Context, scope: Scriptable) = makeFn(scope, "back") { _, _, _, _ ->
        val result = automationDriver.execute(AutomationAction.Back)
        if (result is ActionResult.Failed) {
            scriptError("back: ${result.reason}")
        }
        buildResultObject(cx, scope, "ok" to true)
    }

    private fun homeFn(cx: Context, scope: Scriptable) = makeFn(scope, "home") { _, _, _, _ ->
        val result = automationDriver.execute(AutomationAction.Home)
        if (result is ActionResult.Failed) {
            scriptError("home: ${result.reason}")
        }
        buildResultObject(cx, scope, "ok" to true)
    }

    private fun screenshotFn(cx: Context, scope: Scriptable) = makeFn(scope, "screenshot") { _, _, _, _ ->
        val base64 = screenshotCapture()
            ?: scriptError("screenshot: capture not available or API < 30")
        buildResultObject(cx, scope, "base64" to base64, "mimeType" to "image/png")
    }

    private fun awaitEventFn(cx: Context, scope: Scriptable) = makeFn(scope, "awaitEvent") { _, _, args, _ ->
        val kind = args.getOrNull(0)?.toString()
            ?: scriptError("awaitEvent: expected kind string")
        val opts = args.getOrNull(1) as? NativeObject
        val timeoutMs = (args.getOrNull(2) as? Number)?.toLong() ?: 5_000L
        val pkg = opts?.get("package", opts)?.toString()
        val textContains = opts?.get("textContains", opts)?.toString()
        eventAwaiter(kind, pkg, textContains, timeoutMs)
    }

    private fun logFn(cx: Context, scope: Scriptable) = makeFn(scope, "log") { _, _, args, _ ->
        val msg = args.joinToString(" ") { it?.toString() ?: "null" }
        logs.add(msg)
        Context.getUndefinedValue()
    }

    private fun httpFn(cx: Context, scope: Scriptable) = makeFn(scope, "http") { _, _, args, _ ->
        val opts: NativeObject = args.getOrNull(0) as? NativeObject
            ?: scriptError("http: expected options object")

        val url: String = opts.get("url", opts)?.toString()
            ?: scriptError("http: url is required")
        val method = opts.get("method", opts)?.toString()?.uppercase() ?: "GET"
        val bodyStr = opts.get("body", opts)?.toString()
        val headersObj = opts.get("headers", opts) as? NativeObject

        val reqBuilder = Request.Builder().url(url)
        headersObj?.ids?.filterIsInstance<String>()?.forEach { key ->
            val v = headersObj.get(key, headersObj)?.toString() ?: return@forEach
            reqBuilder.addHeader(key, v)
        }

        val body: RequestBody? = when {
            bodyStr != null -> bodyStr.toRequestBody("application/json".toMediaType())
            method in setOf("POST", "PUT", "PATCH") -> RequestBody.create(null, ByteArray(0))
            else -> null
        }
        reqBuilder.method(method, body)

        val response = okHttpClient.newCall(reqBuilder.build()).execute()
        val responseBody = response.body?.string() ?: ""
        response.close()

        val status = response.code
        cx.evaluateString(scope, "({status: $status, body: ${jsonStringLiteral(responseBody)}})", "<http>", 1, null)
    }

    // ---- helpers ----

    /**
     * Throws a Rhino EcmaError with [message] — stops JS execution and lets the script catch it.
     * Returns [Nothing] so Kotlin infers the correct type for `?:` expressions.
     */
    private fun scriptError(message: String): Nothing {
        Context.reportRuntimeError(message) // throws EcmaError
        error(message) // unreachable; satisfies Kotlin's Nothing type
    }

    private fun parseSelectorFromNative(obj: NativeObject): Selector? {
        val kind = obj.get("kind", obj)?.toString() ?: return null
        val value = obj.get("value", obj)?.toString() ?: return null
        val selectorKind = when (kind) {
            "text" -> SelectorKind.TEXT
            "resource_id" -> SelectorKind.RESOURCE_ID
            "semantic_key" -> SelectorKind.SEMANTIC_KEY
            "target_id" -> SelectorKind.TARGET_ID
            "content_desc" -> SelectorKind.CONTENT_DESC
            "bounds" -> SelectorKind.BOUNDS
            "package_name" -> SelectorKind.PACKAGE_NAME
            "coordinate" -> SelectorKind.COORDINATE
            else -> return null
        }
        return Selector(selectorKind, value)
    }

    private fun parseActionFromNative(obj: NativeObject): AutomationAction? {
        val kind = obj.get("kind", obj)?.toString() ?: return null
        return when (kind) {
            "click" -> {
                val targetObj = obj.get("target", obj) as? NativeObject ?: return null
                val selector = parseSelectorFromNative(targetObj) ?: return null
                AutomationAction.Click(selector)
            }
            "long_press" -> {
                val targetObj = obj.get("target", obj) as? NativeObject ?: return null
                val selector = parseSelectorFromNative(targetObj) ?: return null
                AutomationAction.LongPress(selector)
            }
            "input_text" -> {
                val targetObj = obj.get("target", obj) as? NativeObject ?: return null
                val selector = parseSelectorFromNative(targetObj) ?: return null
                val text = obj.get("inputText", obj)?.toString() ?: ""
                AutomationAction.InputText(selector, text)
            }
            "open_app" -> {
                val targetObj = obj.get("target", obj) as? NativeObject ?: return null
                val pkg = targetObj.get("value", targetObj)?.toString() ?: return null
                AutomationAction.OpenApp(pkg)
            }
            "open_intent" -> {
                val intentAction = obj.get("intentAction", obj)?.toString() ?: return null
                val pkg = obj.get("package", obj)?.toString()
                AutomationAction.OpenIntent(intentAction, pkg)
            }
            "back" -> AutomationAction.Back
            "home" -> AutomationAction.Home
            else -> null
        }
    }

    private fun snapshotContains(snapshot: UiSnapshot, selector: Selector): Boolean =
        snapshot.targets.any { target ->
            when (selector.kind) {
                SelectorKind.TEXT -> target.text == selector.value
                SelectorKind.RESOURCE_ID -> target.resourceId == selector.value
                SelectorKind.SEMANTIC_KEY -> target.semanticKey == selector.value
                SelectorKind.CONTENT_DESC -> target.contentDesc == selector.value
                SelectorKind.PACKAGE_NAME -> target.packageName == selector.value
                else -> false
            }
        }

    private fun buildResultObject(cx: Context, scope: Scriptable, vararg pairs: Pair<String, Any?>): Any {
        val entries = pairs.joinToString(", ") { (k, v) ->
            when (v) {
                is Boolean -> "$k: $v"
                is Number -> "$k: $v"
                is String -> "$k: ${jsonStringLiteral(v)}"
                null -> "$k: null"
                else -> "$k: ${jsonStringLiteral(v.toString())}"
            }
        }
        return cx.evaluateString(scope, "({$entries})", "<result>", 1, null) ?: Unit
    }

    private fun jsonStringLiteral(s: String): String = buildString {
        append('"')
        for (c in s) {
            when (c) {
                '\\' -> append("\\\\")
                '"' -> append("\\\"")
                '\n' -> append("\\n")
                '\r' -> append("\\r")
                '\t' -> append("\\t")
                else -> append(c)
            }
        }
        append('"')
    }

    private fun makeFn(
        scope: Scriptable,
        name: String,
        body: (cx: Context, scope: Scriptable, args: Array<Any?>, thisObj: Scriptable) -> Any?,
    ): BaseFunction = object : BaseFunction() {
        override fun getFunctionName(): String = name
        override fun call(cx: Context, callScope: Scriptable, thisObj: Scriptable, args: Array<Any?>): Any? =
            body(cx, callScope, args, thisObj)
        override fun getArity(): Int = 0
        override fun getParentScope(): Scriptable = scope
        override fun getPrototype(): Scriptable = ScriptableObject.getFunctionPrototype(scope)
    }
}
