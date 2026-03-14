package com.autosdk.agent.agent

import android.util.Log
import com.autosdk.agent.action.ActionResult
import com.autosdk.agent.action.AutomationAction
import com.autosdk.agent.action.ScrollDirection
import com.autosdk.agent.action.Selector
import com.autosdk.agent.action.SelectorKind
import com.autosdk.agent.observation.UiSnapshot
import com.autosdk.agent.observation.UiTarget
import com.autosdk.agent.state.AgentEvent
import com.autosdk.agent.transport.AgentTransport
import com.autosdk.agent.transport.JsonRpcErrorCode
import com.autosdk.agent.transport.JsonRpcRequest
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.add
import kotlinx.serialization.json.addJsonObject
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put

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
    private val onExecutionEvent: suspend (AgentEvent) -> Unit = {},
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
        transport.sendSuccess(req.id, buildJsonArray {
            capabilities.forEach { cap ->
                addJsonObject {
                    put("name", cap["name"] as String)
                    put("available", cap["available"] as Boolean)
                    (cap["reason"] as? String)?.takeIf { it.isNotBlank() }?.let { put("reason", it) }
                }
            }
        })
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

    private fun snapshotToJson(snapshot: UiSnapshot): JsonElement = buildJsonObject {
        put("snapshotId", snapshot.snapshotId)
        put("deviceId", snapshot.deviceId)
        snapshot.packageName?.let { put("packageName", it) }
        snapshot.activityName?.let { put("activityName", it) }
        snapshot.screenState?.let { put("screenState", it) }
        put("capturedAt", snapshot.capturedAt)
        put("targets", buildJsonArray {
            snapshot.targets.forEach { add(targetToJson(it)) }
        })
    }

    private fun targetToJson(target: UiTarget): JsonElement = buildJsonObject {
        put("targetId", target.targetId)
        target.role?.let { put("role", it) }
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

    // ---- parsing helpers ----

    private fun parseSelector(obj: JsonObject): Selector? {
        val kind = obj["kind"]?.jsonPrimitive?.contentOrNull ?: return null
        val value = obj["value"]?.jsonPrimitive?.contentOrNull ?: return null
        val selectorKind = when (kind) {
            "text" -> SelectorKind.TEXT
            "resource_id" -> SelectorKind.RESOURCE_ID
            "target_id" -> SelectorKind.TARGET_ID
            "content_desc" -> SelectorKind.CONTENT_DESC
            "bounds" -> SelectorKind.BOUNDS
            "package_name" -> SelectorKind.PACKAGE_NAME
            else -> return null
        }
        return Selector(selectorKind, value)
    }

    private fun parseAction(obj: JsonObject): AutomationAction? {
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
            "close_app" -> AutomationAction.CloseApp
            "screenshot" -> AutomationAction.Screenshot
            else -> null
        }
    }
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
