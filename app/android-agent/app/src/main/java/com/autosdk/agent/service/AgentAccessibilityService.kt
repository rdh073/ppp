package com.autosdk.agent.service

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.AccessibilityServiceInfo
import android.provider.Settings
import android.util.Log
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityWindowInfo
import com.autosdk.agent.action.ActionExecutor
import com.autosdk.agent.agent.AccessibilityAgentAutomationDriver
import com.autosdk.agent.agent.AgentRuntime
import com.autosdk.agent.observation.SnapshotBuilder
import com.autosdk.agent.state.AgentEvent
import com.autosdk.agent.state.AgentLogger
import com.autosdk.agent.state.AgentRuntimeHooks
import com.autosdk.agent.state.AgentStateStore
import com.autosdk.agent.state.AgentStateCoordinator
import com.autosdk.agent.state.AgentStatus
import com.autosdk.agent.state.CoroutineBackoffScheduler
import com.autosdk.agent.state.CoroutineHeartbeatScheduler
import com.autosdk.agent.state.PendingAccessibilityDisabledEvent
import com.autosdk.agent.state.SharedPreferencesAgentStateStore
import com.autosdk.agent.transport.WebSocketAgentTransport
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import java.util.concurrent.atomic.AtomicLong

private const val TAG = "AgentAccessibilitySvc"
private const val METHOD_ANDROID_ACCESSIBILITY_DISABLED = "android.accessibility.disabled"

/** Debounce window for UI settle detection. */
private const val SETTLE_DEBOUNCE_MS = 250L

/** Hard cap on settle waiting — return after this even if events keep arriving. */
private const val SETTLE_TIMEOUT_MS = 3_000L

/**
 * The accessibility service is the Android execution edge of the agent. It:
 *   1. Tracks the active activity name from window-state events.
 *   2. Provides event-driven UI settle detection for post-action observation.
 *   3. Captures multi-window snapshots so dialogs and permission prompts are visible.
 *   4. Wires runtime, transport, and the local state coordinator.
 *
 * Invariants (from the ADR):
 *   - Raw events are triggers only; truth is always a freshly built UiSnapshot.
 *   - Reconnect/backoff and handshake semantics live in the state coordinator.
 *   - Service interruption surfaces explicitly, not as silent degradation.
 */
class AgentAccessibilityService : AccessibilityService() {

    companion object {
        @Volatile
        internal var instance: AgentAccessibilityService? = null
            private set
    }

    private val serviceScope = CoroutineScope(Dispatchers.IO + SupervisorJob())

    @Volatile private var transport: WebSocketAgentTransport? = null
    @Volatile private var runtime: AgentRuntime? = null
    @Volatile private var coordinator: AgentStateCoordinator? = null
    @Volatile private var stateStore: AgentStateStore? = null

    /** Last known foreground activity class name. Updated on TYPE_WINDOW_STATE_CHANGED. */
    @Volatile private var currentActivityName: String? = null

    /**
     * Epoch-ms of the last accessibility event. Used by awaitSettle to detect
     * when the UI has stopped changing after an action.
     */
    private val lastEventMs = AtomicLong(0L)
    private val outboundEventSeqNo = AtomicLong(0L)
    @Volatile private var wasTransportConnected = false

    override fun onServiceConnected() {
        super.onServiceConnected()
        instance = this
        Log.i(TAG, "Accessibility service connected")

        serviceInfo =
            AccessibilityServiceInfo().apply {
                eventTypes =
                    AccessibilityEvent.TYPE_WINDOW_STATE_CHANGED or
                        AccessibilityEvent.TYPE_WINDOW_CONTENT_CHANGED or
                        AccessibilityEvent.TYPE_WINDOWS_CHANGED or
                        AccessibilityEvent.TYPE_VIEW_TEXT_CHANGED or
                        AccessibilityEvent.TYPE_VIEW_SCROLLED
                feedbackType = AccessibilityServiceInfo.FEEDBACK_GENERIC
                flags =
                    AccessibilityServiceInfo.FLAG_REPORT_VIEW_IDS or
                        AccessibilityServiceInfo.FLAG_RETRIEVE_INTERACTIVE_WINDOWS
                notificationTimeout = 100
            }

        setupAgentRuntime()
    }

    override fun onAccessibilityEvent(event: AccessibilityEvent?) {
        lastEventMs.set(System.currentTimeMillis())

        if (event?.eventType == AccessibilityEvent.TYPE_WINDOW_STATE_CHANGED) {
            val cls = event.className?.toString()
            if (!cls.isNullOrBlank()) {
                currentActivityName = cls
            }
        }
    }

    override fun onInterrupt() {
        Log.w(TAG, "Accessibility service interrupted")
        serviceScope.launch { notifyAccessibilityDisabled("service_interrupted") }
        val activeCoordinator = coordinator
        if (activeCoordinator != null) {
            serviceScope.launch { activeCoordinator.dispatch(AgentEvent.ServiceInterrupted) }
        } else {
            transport?.disconnect()
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        instance = null

        val activeCoordinator = coordinator
        if (activeCoordinator != null) {
            runBlocking {
                activeCoordinator.dispatch(AgentEvent.ServiceDestroyed)
            }
        } else {
            transport?.disconnect()
        }

        serviceScope.cancel()
        Log.i(TAG, "Accessibility service destroyed")
    }

    /**
     * Suspends until the UI appears to have stopped changing, or SETTLE_TIMEOUT_MS elapses.
     *
     * Uses lastEventMs as an event fence: if no new accessibility event has arrived
     * within SETTLE_DEBOUNCE_MS, the UI is considered settled. The hard timeout
     * prevents livelock on continuously animated UIs.
     */
    suspend fun awaitSettle() {
        val deadline = System.currentTimeMillis() + SETTLE_TIMEOUT_MS
        while (System.currentTimeMillis() < deadline) {
            delay(SETTLE_DEBOUNCE_MS)
            val quietFor = System.currentTimeMillis() - lastEventMs.get()
            if (quietFor >= SETTLE_DEBOUNCE_MS) {
                return
            }
        }
        Log.w(TAG, "UI settle timed out after ${SETTLE_TIMEOUT_MS}ms — proceeding with snapshot")
    }

    private fun setupAgentRuntime() {
        val deviceId = Settings.Secure.getString(contentResolver, Settings.Secure.ANDROID_ID)
        val serverUrl = resolveServerUrl()
        val capabilityProvider = { buildCapabilityList() }
        val localStateStore = SharedPreferencesAgentStateStore.from(applicationContext)
        stateStore = localStateStore
        outboundEventSeqNo.set(localStateStore.read().lastOutboundEventSeqNo)

        Log.i(TAG, "Preparing agent runtime for $serverUrl device=$deviceId")

        val executor = ActionExecutor(this)
        val automationDriver = AccessibilityAgentAutomationDriver(executor)
        val ws =
            WebSocketAgentTransport(
                serverUrl = serverUrl,
                scope = serviceScope,
            )
        transport = ws

        lateinit var localCoordinator: AgentStateCoordinator
        val heartbeatScheduler =
            CoroutineHeartbeatScheduler(serviceScope) {
                localCoordinator.dispatch(AgentEvent.HeartbeatTick)
            }
        val backoffScheduler =
            CoroutineBackoffScheduler(serviceScope) {
                localCoordinator.dispatch(AgentEvent.BackoffElapsed)
            }
        localCoordinator =
            AgentStateCoordinator(
                scope = serviceScope,
                deviceId = deviceId,
                store = localStateStore,
                transportDriver = ws,
                heartbeatScheduler = heartbeatScheduler,
                backoffScheduler = backoffScheduler,
                runtimeHooks = ServiceRuntimeHooks(),
                logger = ServiceAgentLogger(),
                capabilitiesProvider = capabilityProvider,
            )
        coordinator = localCoordinator

        val rt =
            AgentRuntime(
                transport = ws,
                snapshotBuilder = { buildSnapshot(deviceId) },
                settle = { awaitSettle() },
                automationDriver = automationDriver,
                deviceId = deviceId,
                capabilities = capabilityProvider(),
                onExecutionEvent = { event -> localCoordinator.dispatch(event) },
            )
        runtime = rt
        rt.start()

        serviceScope.launch {
            localCoordinator.dispatch(AgentEvent.ServiceConnected)
        }
    }

    /**
     * Builds a snapshot from all visible application and system windows.
     *
     * Including multiple windows means permission dialogs, system alerts, and
     * bottom sheets are all visible in a single snapshot.
     *
     * Falls back to rootInActiveWindow when the window list is unavailable.
     */
    private fun buildSnapshot(deviceId: String): com.autosdk.agent.observation.UiSnapshot? {
        val windowRoots =
            windows
                ?.filter { win ->
                    win.type == AccessibilityWindowInfo.TYPE_APPLICATION ||
                        win.type == AccessibilityWindowInfo.TYPE_SYSTEM
                }
                ?.mapNotNull { it.root }
                ?.takeIf { it.isNotEmpty() }
                ?: listOfNotNull(rootInActiveWindow)

        if (windowRoots.isEmpty()) {
            return null
        }

        val foregroundPkg = windowRoots.firstOrNull()?.packageName?.toString()

        return SnapshotBuilder.build(
            roots = windowRoots,
            deviceId = deviceId,
            packageName = foregroundPkg,
            activityName = currentActivityName,
        )
    }

    private fun resolveServerUrl(): String {
        val sysProp =
            runCatching {
                Class.forName("android.os.SystemProperties")
                    .getMethod("get", String::class.java, String::class.java)
                    .invoke(null, "auto.agent.server_url", "") as String
            }.getOrElse { "" }
        return sysProp.takeIf { it.isNotBlank() } ?: com.autosdk.agent.BuildConfig.SERVER_URL
    }

    private fun buildCapabilityList(): List<Map<String, Any>> =
        listOf(
            mapOf("name" to "observe", "available" to true),
            mapOf("name" to "click", "available" to true),
            mapOf("name" to "long_press", "available" to true),
            mapOf("name" to "input_text", "available" to true),
            mapOf("name" to "delete_text", "available" to true),
            mapOf("name" to "scroll", "available" to true),
            mapOf("name" to "home", "available" to true),
            mapOf("name" to "back", "available" to true),
            mapOf("name" to "wake", "available" to true),
            mapOf("name" to "open_app", "available" to true),
            mapOf("name" to "close_app", "available" to true),
            mapOf("name" to "fill_form", "available" to true),
            mapOf(
                "name" to "screenshot",
                "available" to (android.os.Build.VERSION.SDK_INT >= 30),
                "reason" to if (android.os.Build.VERSION.SDK_INT < 30) "Requires API 30+" else "",
            ),
        )

    private suspend fun notifyAccessibilityDisabled(reason: String) {
        val seqNo = outboundEventSeqNo.incrementAndGet()
        val pendingEvent = PendingAccessibilityDisabledEvent(seqNo = seqNo, reason = reason)
        stateStore?.persistLastOutboundEventSeqNo(seqNo)
        stateStore?.persistPendingAccessibilityDisabledEvent(pendingEvent)
        flushPendingAccessibilityDisabledEvent()
    }

    private suspend fun flushPendingAccessibilityDisabledEvent() {
        val store = stateStore ?: return
        val pendingEvent = store.read().pendingAccessibilityDisabledEvent ?: return
        val payload =
            buildJsonObject {
                put("seqNo", pendingEvent.seqNo)
                put("reason", pendingEvent.reason)
            }

        val sent =
            runCatching {
                transport?.sendNotification(
                    method = METHOD_ANDROID_ACCESSIBILITY_DISABLED,
                    params = payload,
                ) == true
            }.getOrElse { error ->
                Log.w(TAG, "Failed to notify accessibility disabled: ${error.message}")
                false
            }

        if (sent) {
            store.clearPendingAccessibilityDisabledEvent()
        }
    }

    private inner class ServiceRuntimeHooks : AgentRuntimeHooks {
        override fun clearInflightCommand() {
            // Runtime execution event wiring will own this more precisely in PR6.
        }

        override fun onStatusChanged(status: AgentStatus) {
            val isConnected = status.transport == "connected"
            if (isConnected && !wasTransportConnected) {
                serviceScope.launch { flushPendingAccessibilityDisabledEvent() }
            }
            wasTransportConnected = isConnected
            Log.d(TAG, "status ${status.toDebugString()}")
        }
    }

    private inner class ServiceAgentLogger : AgentLogger {
        override fun log(message: String) {
            Log.i(TAG, message)
        }
    }
}
