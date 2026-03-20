package com.autosdk.agent.service

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.AccessibilityServiceInfo
import android.os.Build
import android.provider.Settings
import android.util.Log
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityWindowInfo
import com.autosdk.agent.action.ActionExecutor
import com.autosdk.agent.agent.AccessibilityAgentAutomationDriver
import com.autosdk.agent.agent.AgentCapabilities
import com.autosdk.agent.agent.AgentRuntime
import com.autosdk.agent.observation.SnapshotBuilder
import com.autosdk.agent.state.AgentEvent
import com.autosdk.agent.state.AgentLogger
import com.autosdk.agent.state.AgentRuntimeHooks
import com.autosdk.agent.state.AgentState
import com.autosdk.agent.state.AgentStateStore
import com.autosdk.agent.state.AgentStateCoordinator
import com.autosdk.agent.state.AgentStatus
import com.autosdk.agent.state.AgentTransportPhase
import com.autosdk.agent.state.CoroutineBackoffScheduler
import com.autosdk.agent.state.CoroutineHeartbeatScheduler
import com.autosdk.agent.state.PendingAccessibilityDisabledEvent
import com.autosdk.agent.state.SharedPreferencesAgentStateStore
import com.autosdk.agent.state.toStatus
import com.autosdk.agent.transport.WebSocketAgentTransport
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.add
import kotlinx.serialization.json.addJsonObject
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import java.util.concurrent.atomic.AtomicBoolean
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

    /** Device ID set during setupAgentRuntime(); used by the debounced publish coroutine. */
    @Volatile private var agentDeviceId: String? = null

    /** Inflight debounce job for proactive android.screen.changed notifications. */
    @Volatile private var pendingScreenChangeJob: Job? = null

    /**
     * Epoch-ms of the last accessibility event. Used by awaitSettle to detect
     * when the UI has stopped changing after an action.
     */
    private val lastEventMs = AtomicLong(0L)
    private val outboundEventSeqNo = AtomicLong(0L)
    @Volatile private var wasTransportConnected = false

    /**
     * Guards against double-initialisation. Uses CAS so that exactly one of
     * [onServiceConnected] or the first [onAccessibilityEvent] wins the race.
     * Needed because some Android environments (e.g. Waydroid) omit the
     * [onServiceConnected] callback when the process restarts while the service
     * is already enabled — causing [coordinator] to remain null indefinitely.
     */
    private val runtimeInitialized = AtomicBoolean(false)

    override fun onServiceConnected() {
        super.onServiceConnected()
        instance = this
        AgentNotificationManager.createChannel(this)
        Log.i(TAG, "Accessibility service connected")

        serviceInfo =
            AccessibilityServiceInfo().apply {
                eventTypes =
                    AccessibilityEvent.TYPE_WINDOW_STATE_CHANGED or
                        AccessibilityEvent.TYPE_WINDOW_CONTENT_CHANGED or
                        AccessibilityEvent.TYPE_WINDOWS_CHANGED or
                        AccessibilityEvent.TYPE_VIEW_TEXT_CHANGED or
                        AccessibilityEvent.TYPE_VIEW_SCROLLED or
                        AccessibilityEvent.TYPE_NOTIFICATION_STATE_CHANGED
                feedbackType = AccessibilityServiceInfo.FEEDBACK_GENERIC
                flags =
                    AccessibilityServiceInfo.FLAG_REPORT_VIEW_IDS or
                        AccessibilityServiceInfo.FLAG_RETRIEVE_INTERACTIVE_WINDOWS
                notificationTimeout = 100
            }

        if (runtimeInitialized.compareAndSet(false, true)) {
            setupAgentRuntime()
        }
        AgentNotificationManager.update(this, coordinator?.currentStatus() ?: AgentState.initial().toStatus())
    }

    override fun onAccessibilityEvent(event: AccessibilityEvent?) {
        lastEventMs.set(System.currentTimeMillis())

        // Bootstrap guard: if the process was restarted while the accessibility
        // service was already enabled, Android skips onServiceConnected() on the
        // new instance.  Detect this from the first arriving event and
        // initialise the runtime lazily.  CAS ensures setupAgentRuntime() runs
        // exactly once regardless of which path fires first.
        if (runtimeInitialized.compareAndSet(false, true)) {
            Log.i(TAG, "Runtime not initialised — bootstrapping from first accessibility event")
            setupAgentRuntime()
        }

        when (event?.eventType) {
            AccessibilityEvent.TYPE_WINDOW_STATE_CHANGED -> {
                val cls = event.className?.toString()
                if (!cls.isNullOrBlank()) {
                    currentActivityName = cls
                }
                // Dispatch ActivityCreated immediately — no settle wait, no digest dedup.
                // Fires on every window transition so workflows can trigger on activity
                // changes even when semantic content is unchanged.
                val seqNo = outboundEventSeqNo.incrementAndGet()
                stateStore?.persistLastOutboundEventSeqNo(seqNo)
                val params = buildActivityCreatedParams(
                    seqNo = seqNo,
                    packageName = event.packageName?.toString(),
                    className = cls,
                )
                serviceScope.launch {
                    coordinator?.dispatch(AgentEvent.ActivityCreated(params))
                }
            }
            AccessibilityEvent.TYPE_NOTIFICATION_STATE_CHANGED -> {
                val seqNo = outboundEventSeqNo.incrementAndGet()
                stateStore?.persistLastOutboundEventSeqNo(seqNo)
                val params = buildNotificationParams(seqNo = seqNo, event = event)
                serviceScope.launch {
                    coordinator?.dispatch(AgentEvent.NotificationReceived(params))
                }
            }
        }

        if (event != null && shouldScheduleSemanticPublish(event.eventType)) {
            scheduleSemanticPublish(eventType = event.eventType)
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
        AgentNotificationManager.cancel(this)

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
        agentDeviceId = deviceId
        val serverUrl = resolveServerUrl()
        val capabilityProvider = { AgentCapabilities.buildCapabilityList() }
        val deviceMetadataProvider = {
            mapOf(
                "manufacturer" to Build.MANUFACTURER,
                "model" to Build.MODEL,
                "device" to Build.DEVICE,
                "brand" to Build.BRAND,
                "product" to Build.PRODUCT,
                "androidVersion" to (Build.VERSION.RELEASE ?: ""),
                "sdkInt" to Build.VERSION.SDK_INT,
            )
        }
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
                deviceMetadataProvider = deviceMetadataProvider,
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
        val allWindows = windows
        val filteredWindows =
            allWindows
                ?.filter { win ->
                    win.type == AccessibilityWindowInfo.TYPE_APPLICATION ||
                        win.type == AccessibilityWindowInfo.TYPE_SYSTEM
                }

        val windowRoots =
            filteredWindows
                ?.mapNotNull { it.root }
                ?.takeIf { it.isNotEmpty() }
                ?: listOfNotNull(rootInActiveWindow)

        if (windowRoots.isEmpty()) {
            return null
        }

        val foregroundPkg = windowRoots.firstOrNull()?.packageName?.toString()
        val hasSystemWindow = filteredWindows?.any { it.type == AccessibilityWindowInfo.TYPE_SYSTEM } == true

        return SnapshotBuilder.build(
            roots = windowRoots,
            deviceId = deviceId,
            packageName = foregroundPkg,
            activityName = currentActivityName,
            hasSystemWindow = hasSystemWindow,
        )
    }

    private fun scheduleSemanticPublish(eventType: Int) {
        pendingScreenChangeJob?.cancel()
        pendingScreenChangeJob =
            serviceScope.launch {
                awaitSettle()
                val deviceId = agentDeviceId ?: return@launch
                val snapshot = buildSnapshot(deviceId) ?: return@launch
                val seqNo = outboundEventSeqNo.incrementAndGet()
                stateStore?.persistLastOutboundEventSeqNo(seqNo)
                val params =
                    buildScreenChangedParams(
                        seqNo = seqNo,
                        eventType = eventTypeName(eventType),
                        snapshot = snapshot,
                    )
                coordinator?.dispatch(AgentEvent.WindowStateChanged(params))
            }
    }

    private fun buildScreenChangedParams(
        seqNo: Long,
        eventType: String,
        snapshot: com.autosdk.agent.observation.UiSnapshot,
    ) = buildJsonObject {
        put("seqNo", seqNo)
        snapshot.packageName?.let { put("packageName", it) }
        snapshot.activityName?.let { put("className", it) }
        put("eventType", eventType)
        snapshot.screenState?.let { put("screenState", it) }
        snapshot.focusedTargetId?.let { put("focusedTargetId", it) }
        put("text", buildJsonArray {
            snapshot.targets.forEach { target ->
                target.text?.takeIf { it.isNotBlank() }?.let { add(it) }
                target.label?.takeIf { it.isNotBlank() }?.let { add(it) }
            }
        })
        put(
            "ui",
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
                            addJsonObject {
                                put("formKey", form.formKey)
                                put("fieldKeys", buildJsonArray {
                                    form.fieldKeys.forEach { add(it) }
                                })
                                form.focusedFieldKey?.let { put("focusedFieldKey", it) }
                                put("ready", form.ready)
                            }
                        }
                    },
                )
                put(
                    "buttons",
                    buildJsonArray {
                        snapshot.semantic.buttons.forEach { button ->
                            addJsonObject {
                                put("buttonKey", button.buttonKey)
                                put("enabled", button.enabled)
                                put("visible", button.visible)
                                put("primary", button.primary)
                            }
                        }
                    },
                )
            },
        )
        put("targets", buildJsonArray {
            snapshot.targets.forEach { target ->
                addJsonObject {
                    put("targetId", target.targetId)
                    put("uiRole", target.uiRole)
                    target.label?.let { put("label", it) }
                    target.semanticKey?.let { put("semanticKey", it) }
                    target.formKey?.let { put("formKey", it) }
                    target.text?.let { put("text", it) }
                    target.resourceId?.let { put("resourceId", it) }
                    put("enabled", target.enabled)
                    put("actionable", target.actionable)
                    target.checked?.let { put("checked", it) }
                    put("focused", target.focused)
                }
            }
        })
    }

    private fun buildActivityCreatedParams(
        seqNo: Long,
        packageName: String?,
        className: String?,
    ) = buildJsonObject {
        put("seqNo", seqNo)
        packageName?.let { put("packageName", it) }
        className?.let { put("className", it) }
    }

    private fun buildNotificationParams(
        seqNo: Long,
        event: AccessibilityEvent,
    ) = buildJsonObject {
        put("seqNo", seqNo)
        event.packageName?.toString()?.let { put("packageName", it) }
        put("text", buildJsonArray {
            event.text?.forEach { t -> t?.toString()?.takeIf { it.isNotBlank() }?.let { add(it) } }
            event.contentDescription?.toString()?.takeIf { it.isNotBlank() }?.let { add(it) }
        })
    }

    private fun shouldScheduleSemanticPublish(eventType: Int): Boolean =
        eventType == AccessibilityEvent.TYPE_WINDOW_STATE_CHANGED ||
            eventType == AccessibilityEvent.TYPE_WINDOW_CONTENT_CHANGED ||
            eventType == AccessibilityEvent.TYPE_WINDOWS_CHANGED ||
            eventType == AccessibilityEvent.TYPE_VIEW_TEXT_CHANGED ||
            eventType == AccessibilityEvent.TYPE_VIEW_SCROLLED

    private fun eventTypeName(eventType: Int): String =
        when (eventType) {
            AccessibilityEvent.TYPE_WINDOW_STATE_CHANGED -> "TYPE_WINDOW_STATE_CHANGED"
            AccessibilityEvent.TYPE_WINDOW_CONTENT_CHANGED -> "TYPE_WINDOW_CONTENT_CHANGED"
            AccessibilityEvent.TYPE_WINDOWS_CHANGED -> "TYPE_WINDOWS_CHANGED"
            AccessibilityEvent.TYPE_VIEW_TEXT_CHANGED -> "TYPE_VIEW_TEXT_CHANGED"
            AccessibilityEvent.TYPE_VIEW_SCROLLED -> "TYPE_VIEW_SCROLLED"
            else -> "TYPE_UNKNOWN"
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

    /**
     * Requests an immediate reconnect from the current transport state.
     * Called by [AgentReconnectReceiver] when the user taps "Reconnect" in the
     * persistent status notification.
     *
     * - DISCONNECTED → dispatches [AgentEvent.ConnectRequested]
     * - BACKOFF_WAIT → dispatches [AgentEvent.BackoffElapsed] to skip the remaining delay
     * - Any other state → no-op (already connecting or connected)
     */
    internal fun requestReconnect() {
        val activeCoordinator = coordinator ?: return
        serviceScope.launch {
            when (activeCoordinator.currentState().transport) {
                AgentTransportPhase.DISCONNECTED ->
                    activeCoordinator.dispatch(AgentEvent.ConnectRequested)
                AgentTransportPhase.BACKOFF_WAIT ->
                    activeCoordinator.dispatch(AgentEvent.BackoffElapsed)
                else -> Unit
            }
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
            AgentNotificationManager.update(this@AgentAccessibilityService, status)
            Log.d(TAG, "status ${status.toDebugString()}")
        }
    }

    private inner class ServiceAgentLogger : AgentLogger {
        override fun log(message: String) {
            Log.i(TAG, message)
        }
    }
}
