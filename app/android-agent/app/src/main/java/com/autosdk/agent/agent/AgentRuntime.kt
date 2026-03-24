package com.autosdk.agent.agent

import android.util.Log
import com.autosdk.agent.action.ActionResult
import com.autosdk.agent.action.AutomationAction
import com.autosdk.agent.action.FieldFill
import com.autosdk.agent.action.ScrollDirection
import com.autosdk.agent.action.Selector
import com.autosdk.agent.action.SelectorKind
import com.autosdk.agent.agent.script.JsAutomationBridge
import com.autosdk.agent.agent.script.JsRuntime
import com.autosdk.agent.observation.UiSnapshot
import com.autosdk.agent.observation.UiTarget
import com.autosdk.agent.state.AgentEvent
import com.autosdk.agent.transport.AgentTransport
import com.autosdk.agent.transport.JsonRpcErrorCode
import com.autosdk.agent.transport.JsonRpcRequest
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.add
import kotlinx.serialization.json.addJsonObject
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull
import kotlinx.serialization.json.put
import okhttp3.OkHttpClient

private const val TAG = "AgentRuntime"

/**
 * Dispatches inbound JSON-RPC requests from the control server to the
 * appropriate agent subsystem and sends back results via [transport].
 *
 * Handled methods (server → agent):
 *   - `device.observe`           → build and return the current [UiSnapshot]
 *   - `device.query`             → resolve a selector and return matching [UiTarget]s
 *   - `device.execute`           → observe → act → settle → observe, return both snapshots
 *   - `device.capabilities.get`  → return the agent's capability list
 *
 * The agent never owns task/workflow lifecycle. That belongs to the server.
 */
class AgentRuntime(
    private val transport: AgentTransport,
    /** Returns a fresh multi-window snapshot, or null if the service is not ready. */
    private val snapshotBuilder: () -> UiSnapshot?,
    /** Suspends until the UI appears settled after an action. */
    private val settle: suspend () -> Unit,
    private val automationDriver: AgentAutomationDriver,
    private val deviceId: String,
    private val capabilities: List<Map<String, Any>>,
    private val okHttpClient: OkHttpClient = OkHttpClient(),
    private val onExecutionEvent: suspend (AgentEvent) -> Unit = {},
    private val screenshotCapture: () -> String? = { null },
    private val eventAwaiter: (kind: String, pkg: String?, textContains: String?, timeoutMs: Long) -> Boolean = { _, _, _, _ -> false },
) {

    fun start() {
        transport.onRequest { req -> handleRequest(req) }
        logInfo("AgentRuntime started for device=$deviceId")
    }

    // ---- dispatch ----

    private suspend fun handleRequest(req: JsonRpcRequest) {
        logDebug("→ ${req.method} id=${req.id}")
        try {
            when (req.method) {
                "device.observe" -> handleObserve(req)
                "device.query" -> handleQuery(req)
                "device.execute" -> handleExecute(req)
                "device.capabilities.get" -> handleCapabilitiesGet(req)
                "device.script" -> handleScript(req)
                else -> transport.sendError(
                    req.id,
                    JsonRpcErrorCode.METHOD_NOT_FOUND,
                    "Method not found: ${req.method}",
                )
            }
        } catch (e: Exception) {
            logError("Unhandled error for ${req.method}", e)
            transport.sendError(req.id, JsonRpcErrorCode.INTERNAL_ERROR, e.message ?: "Internal error")
        }
    }

    // ---- handlers ----

    private suspend fun handleObserve(req: JsonRpcRequest) {
        onExecutionEvent(AgentEvent.ObserveStarted(req.id))

        try {
            val snapshot = snapshotBuilder() ?: run {
                logWarn("service_active_no_snapshot:method=device.observe request=${req.id}")
                onExecutionEvent(
                    AgentEvent.ObserveFailed(req.id, "Accessibility tree not available"),
                )
                sendTrackedError(
                    id = req.id,
                    code = JsonRpcErrorCode.DEVICE_UNAVAILABLE,
                    message = "Accessibility tree not available",
                )
                return
            }

            onExecutionEvent(AgentEvent.ObserveFinished(req.id))
            sendTrackedSuccess(req.id, snapshotToJson(snapshot))
        } catch (e: Exception) {
            onExecutionEvent(AgentEvent.ObserveFailed(req.id, e.message ?: "Internal error"))
            sendTrackedError(
                id = req.id,
                code = JsonRpcErrorCode.INTERNAL_ERROR,
                message = e.message ?: "Internal error",
            )
        }
    }

    private suspend fun handleQuery(req: JsonRpcRequest) {
        onExecutionEvent(AgentEvent.QueryStarted(req.id))

        try {
            val params = req.params.jsonObject
            val selectorJson = params["selector"]?.jsonObject ?: run {
                onExecutionEvent(AgentEvent.QueryFailed(req.id, "Missing 'selector'"))
                sendTrackedError(req.id, JsonRpcErrorCode.INVALID_PARAMS, "Missing 'selector'")
                return
            }
            val selector = parseSelector(selectorJson) ?: run {
                onExecutionEvent(AgentEvent.QueryFailed(req.id, "Invalid selector kind"))
                sendTrackedError(req.id, JsonRpcErrorCode.INVALID_PARAMS, "Invalid selector kind")
                return
            }

            val match = automationDriver.findQueryMatch(selector)
            if (match == null) {
                onExecutionEvent(AgentEvent.QueryFinished(req.id))
                sendTrackedSuccess(req.id, buildJsonArray { })
                return
            }

            val snapshot = snapshotBuilder()
            val target =
                snapshot?.targets?.find { t ->
                    !t.resourceId.isNullOrBlank() && t.resourceId == match.resourceId
                } ?: snapshot?.targets?.find { t ->
                    !t.text.isNullOrBlank() && t.text == match.text
                }

            onExecutionEvent(AgentEvent.QueryFinished(req.id))
            sendTrackedSuccess(
                req.id,
                buildJsonArray {
                    if (target != null) {
                        add(targetToJson(target))
                    }
                },
            )
        } catch (e: Exception) {
            onExecutionEvent(AgentEvent.QueryFailed(req.id, e.message ?: "Internal error"))
            sendTrackedError(
                id = req.id,
                code = JsonRpcErrorCode.INTERNAL_ERROR,
                message = e.message ?: "Internal error",
            )
        }
    }

    private suspend fun handleExecute(req: JsonRpcRequest) {
        onExecutionEvent(AgentEvent.ExecuteRequested(req.id))

        try {
            val params = req.params.jsonObject

            val actionJson = params["action"]?.jsonObject ?: run {
                onExecutionEvent(AgentEvent.SnapshotBeforeFailed(req.id, "Missing 'action'"))
                sendTrackedError(req.id, JsonRpcErrorCode.INVALID_PARAMS, "Missing 'action'")
                return
            }
            val action = parseAction(actionJson) ?: run {
                onExecutionEvent(AgentEvent.SnapshotBeforeFailed(req.id, "Unknown action kind"))
                sendTrackedError(req.id, JsonRpcErrorCode.INVALID_PARAMS, "Unknown action kind")
                return
            }

            val snapshotBefore = snapshotBuilder() ?: run {
                logWarn("service_active_no_snapshot:method=device.execute phase=before request=${req.id}")
                onExecutionEvent(
                    AgentEvent.SnapshotBeforeFailed(req.id, "Accessibility tree not available"),
                )
                sendTrackedError(
                    req.id,
                    JsonRpcErrorCode.DEVICE_UNAVAILABLE,
                    "Accessibility tree not available",
                )
                return
            }
            onExecutionEvent(AgentEvent.SnapshotBeforeBuilt(req.id))

            when (val result = automationDriver.execute(action)) {
                is ActionResult.Failed -> {
                    onExecutionEvent(AgentEvent.ActionFailed(req.id, result.reason))
                    sendTrackedError(req.id, result.toErrorCode(), result.reason)
                    return
                }

                is ActionResult.Ok -> onExecutionEvent(AgentEvent.ActionSucceeded(req.id))
            }

            settle()
            onExecutionEvent(AgentEvent.SettleCompleted(req.id, timedOut = false))

            val snapshotAfter =
                snapshotBuilder() ?: run {
                    logWarn("service_active_no_snapshot:method=device.execute phase=after request=${req.id}")
                    snapshotBefore
                }
            onExecutionEvent(AgentEvent.SnapshotAfterBuilt(req.id))

            sendTrackedSuccess(
                req.id,
                buildJsonObject {
                    put("snapshotBefore", snapshotToJson(snapshotBefore))
                    put("snapshotAfter", snapshotToJson(snapshotAfter))
                },
            )
        } catch (e: Exception) {
            onExecutionEvent(AgentEvent.ActionFailed(req.id, e.message ?: "Internal error"))
            sendTrackedError(
                id = req.id,
                code = JsonRpcErrorCode.INTERNAL_ERROR,
                message = e.message ?: "Internal error",
            )
        }
    }

    private suspend fun handleCapabilitiesGet(req: JsonRpcRequest) {
        transport.sendSuccess(req.id, AgentCapabilities.capabilitiesToJson(capabilities))
    }

    private suspend fun handleScript(req: JsonRpcRequest) {
        val params = req.params.jsonObject
        val source = params["script"]?.jsonPrimitive?.contentOrNull
            ?: run {
                sendTrackedError(req.id, JsonRpcErrorCode.INVALID_PARAMS, "Missing 'script'")
                return
            }
        val scriptParams = params["params"]?.jsonObject
            ?.entries?.associate { (k, v) -> k to (v.jsonPrimitive.contentOrNull ?: "") }
            ?: emptyMap()
        val timeoutMs = params["timeout"]?.jsonPrimitive?.longOrNull ?: 30_000L

        val logs = mutableListOf<String>()
        val bridge = JsAutomationBridge(snapshotBuilder, automationDriver, okHttpClient, logs, screenshotCapture, eventAwaiter)
        val runtime = JsRuntime(bridge)

        val result = withContext(Dispatchers.IO) {
            runtime.execute(source, scriptParams, timeoutMs)
        }

        result.fold(
            onSuccess = { r ->
                sendTrackedSuccess(
                    req.id,
                    buildJsonObject {
                        put("output", buildJsonObject {
                            r.output.forEach { (k, v) ->
                                put(k, anyToJsonElement(v))
                            }
                        })
                        put("logs", buildJsonArray { r.logs.forEach { add(it) } })
                        put("durationMs", r.durationMs)
                    },
                )
            },
            onFailure = { e ->
                sendTrackedError(req.id, JsonRpcErrorCode.SCRIPT_ERROR, e.message ?: "Script error")
            },
        )
    }

    private suspend fun sendTrackedSuccess(
        id: String,
        result: JsonElement,
    ) {
        transport.sendSuccess(id, result)
        onExecutionEvent(AgentEvent.ResponseSent(id))
    }

    private suspend fun sendTrackedError(
        id: String,
        code: Int,
        message: String,
    ) {
        transport.sendError(id, code, message)
        onExecutionEvent(AgentEvent.ResponseSent(id))
    }

    // ---- serialisation helpers ----

    private fun snapshotToJson(snapshot: UiSnapshot): JsonElement =
        UiSnapshotSerializer.toJson(snapshot)

    private fun targetToJson(target: UiTarget): JsonElement =
        UiSnapshotSerializer.targetToJson(target)

    // ---- parsing helpers ----

    private fun parseSelector(obj: JsonObject): Selector? =
        AgentProtocolDeserializer.parseSelector(obj)

    private fun parseAction(obj: JsonObject): AutomationAction? =
        AgentProtocolDeserializer.parseAction(obj)
}

// ---- extension ----

private fun ActionResult.Failed.toErrorCode(): Int = when (failureClass) {
    "target_not_found" -> JsonRpcErrorCode.TARGET_NOT_FOUND
    "target_not_actionable" -> JsonRpcErrorCode.TARGET_NOT_ACTIONABLE
    "capability_unavailable" -> JsonRpcErrorCode.CAPABILITY_UNAVAILABLE
    "input_rejected" -> JsonRpcErrorCode.INPUT_REJECTED
    "device_unavailable" -> JsonRpcErrorCode.DEVICE_UNAVAILABLE
    else -> JsonRpcErrorCode.INTERNAL_ERROR
}

private fun logInfo(message: String) {
    runCatching { Log.i(TAG, message) }
}

private fun logDebug(message: String) {
    runCatching { Log.d(TAG, message) }
}

private fun logError(
    message: String,
    error: Throwable,
) {
    runCatching { Log.e(TAG, message, error) }
}

private fun logWarn(message: String) {
    runCatching { Log.w(TAG, message) }
}

private fun anyToJsonElement(value: Any?): JsonElement = when (value) {
    null -> JsonNull
    is Boolean -> JsonPrimitive(value)
    is Number -> JsonPrimitive(value.toDouble())
    is String -> JsonPrimitive(value)
    is List<*> -> buildJsonArray { value.forEach { add(anyToJsonElement(it)) } }
    is Map<*, *> -> buildJsonObject {
        value.forEach { (k, v) -> put(k.toString(), anyToJsonElement(v)) }
    }
    else -> JsonPrimitive(value.toString())
}

// ---- serializers ----

/**
 * Serializes [UiSnapshot] and [UiTarget] to JSON for the agent wire protocol.
 * Extracted so snapshot serialization can be tested without constructing a full runtime.
 */
internal object UiSnapshotSerializer {
    fun toJson(snapshot: UiSnapshot): JsonElement = buildJsonObject {
        put("snapshotId", snapshot.snapshotId)
        put("deviceId", snapshot.deviceId)
        snapshot.packageName?.let { put("packageName", it) }
        snapshot.activityName?.let { put("activityName", it) }
        snapshot.screenState?.let { put("screenState", it) }
        snapshot.focusedTargetId?.let { put("focusedTargetId", it) }
        put(
            "semantic",
            buildJsonObject {
                put("activeUiKey", snapshot.semantic.activeUiKey)
                put("baseScreenKey", snapshot.semantic.baseScreenKey)
                snapshot.semantic.overlayKey?.let { put("overlayKey", it) }
                put("uiReady", snapshot.semantic.uiReady)
                put("semanticDigest", snapshot.semantic.semanticDigest)
                snapshot.semantic.focusedTargetKey?.let { put("focusedTargetKey", it) }
                put(
                    "forms",
                    buildJsonArray {
                        snapshot.semantic.forms.forEach { form ->
                            add(
                                buildJsonObject {
                                    put("formKey", form.formKey)
                                    put("fieldKeys", buildJsonArray {
                                        form.fieldKeys.forEach { add(it) }
                                    })
                                    form.focusedFieldKey?.let { put("focusedFieldKey", it) }
                                    put("ready", form.ready)
                                },
                            )
                        }
                    },
                )
                put(
                    "buttons",
                    buildJsonArray {
                        snapshot.semantic.buttons.forEach { button ->
                            add(
                                buildJsonObject {
                                    put("buttonKey", button.buttonKey)
                                    put("enabled", button.enabled)
                                    put("visible", button.visible)
                                    put("primary", button.primary)
                                },
                            )
                        }
                    },
                )
            },
        )
        put("capturedAt", snapshot.capturedAt)
        put("targets", buildJsonArray {
            snapshot.targets.forEach { add(targetToJson(it)) }
        })
    }

    fun targetToJson(target: UiTarget): JsonElement = buildJsonObject {
        put("targetId", target.targetId)
        target.role?.let { put("role", it) }
        put("uiRole", target.uiRole)
        target.label?.let { put("label", it) }
        target.semanticKey?.let { put("semanticKey", it) }
        target.formKey?.let { put("formKey", it) }
        target.text?.let { put("text", it) }
        target.resourceId?.let { put("resourceId", it) }
        target.packageName?.let { put("packageName", it) }
        put("bounds", buildJsonArray { target.bounds.forEach { add(it) } })
        put("actionable", target.actionable)
        put("enabled", target.enabled)
        target.checked?.let { put("checked", it) }
        put("selected", target.selected)
        put("scrollable", target.scrollable)
        put("focused", target.focused)
        put("password", target.password)
        // contentDesc omitted — not in the contracts UiTargetSchema wire format.
    }
}

/**
 * Deserializes selector and action objects from the agent wire protocol.
 * Extracted so protocol parsing can be tested without constructing a full runtime.
 */
internal object AgentProtocolDeserializer {
    fun parseSelector(obj: JsonObject): Selector? {
        val kind = obj["kind"]?.jsonPrimitive?.contentOrNull ?: return null
        val value = obj["value"]?.jsonPrimitive?.contentOrNull ?: return null
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

    fun parseAction(obj: JsonObject): AutomationAction? {
        val kind = obj["kind"]?.jsonPrimitive?.contentOrNull ?: return null
        return when (kind) {
            "click" -> AutomationAction.Click(
                parseSelector(obj["target"]?.jsonObject ?: return null) ?: return null
            )
            "long_press" -> AutomationAction.LongPress(
                parseSelector(obj["target"]?.jsonObject ?: return null) ?: return null
            )
            "input_text" -> AutomationAction.InputText(
                selector = parseSelector(obj["target"]?.jsonObject ?: return null) ?: return null,
                value = obj["inputText"]?.jsonPrimitive?.contentOrNull ?: "",
            )
            "delete_text" -> AutomationAction.DeleteText(
                parseSelector(obj["target"]?.jsonObject ?: return null) ?: return null
            )
            "drag" -> AutomationAction.Drag(
                parseSelector(obj["target"]?.jsonObject ?: return null) ?: return null
            )
            "scroll" -> {
                val dir = when (obj["direction"]?.jsonPrimitive?.contentOrNull) {
                    "backward" -> ScrollDirection.BACKWARD
                    else -> ScrollDirection.FORWARD
                }
                AutomationAction.Scroll(
                    selector = obj["target"]?.jsonObject?.let { parseSelector(it) },
                    direction = dir,
                )
            }
            "home" -> AutomationAction.Home
            "back" -> AutomationAction.Back
            "wake" -> AutomationAction.Wake
            "open_app" -> {
                val pkgName = obj["target"]?.jsonObject
                    ?.get("value")?.jsonPrimitive?.contentOrNull ?: return null
                AutomationAction.OpenApp(pkgName)
            }
            "open_intent" -> {
                val intentAction = obj["intentAction"]?.jsonPrimitive?.contentOrNull ?: return null
                val packageName = obj["package"]?.jsonPrimitive?.contentOrNull
                AutomationAction.OpenIntent(intentAction, packageName)
            }
            "swipe" -> AutomationAction.Swipe(
                startX = obj["startX"]?.jsonPrimitive?.content?.toIntOrNull() ?: return null,
                startY = obj["startY"]?.jsonPrimitive?.content?.toIntOrNull() ?: return null,
                endX = obj["endX"]?.jsonPrimitive?.content?.toIntOrNull() ?: return null,
                endY = obj["endY"]?.jsonPrimitive?.content?.toIntOrNull() ?: return null,
                durationMs = obj["durationMs"]?.jsonPrimitive?.content?.toLongOrNull() ?: 300L,
            )
            "close_app" -> AutomationAction.CloseApp
            "screenshot" -> AutomationAction.Screenshot
            "fill_form" -> {
                val jsonFields = obj["fields"]?.jsonArray ?: return null
                val fields = jsonFields.mapNotNull { element ->
                    val fieldObj = element.jsonObject
                    val selector = parseSelector(fieldObj["target"]?.jsonObject ?: return@mapNotNull null)
                        ?: return@mapNotNull null
                    val value = fieldObj["value"]?.jsonPrimitive?.contentOrNull ?: ""
                    FieldFill(selector, value)
                }
                if (fields.isEmpty()) return null
                AutomationAction.FillForm(fields)
            }
            else -> null
        }
    }
}
