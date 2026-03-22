package com.autosdk.agent.service

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.AccessibilityServiceInfo
import android.util.Log
import android.view.accessibility.AccessibilityEvent
import com.autosdk.agent.agent.AgentRuntime
import com.autosdk.agent.state.AgentEvent
import com.autosdk.agent.state.AgentState
import com.autosdk.agent.state.AgentStateCoordinator
import com.autosdk.agent.state.AgentStateStore
import com.autosdk.agent.state.AgentTransportPhase
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
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

private const val TAG = "AgentAccessibilitySvc"

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
    private val eventAwaiter = AccessibilityEventAwaiter(TAG)
    private val snapshotProvider by lazy {
        AccessibilitySnapshotProvider(this) { currentActivityName }
    }
    private val accessibilityDisabledNotifier by lazy {
        AccessibilityDisabledNotifier(
            logTag = TAG,
            stateStoreProvider = { stateStore },
            transportDriverProvider = { transport },
            nextSeqNo = { outboundEventSeqNo.incrementAndGet() },
        )
    }
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
                val params = AccessibilityEventPayloadFactory.buildActivityCreatedParams(
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
                val params = AccessibilityEventPayloadFactory.buildNotificationParams(seqNo = seqNo, event = event)
                serviceScope.launch {
                    coordinator?.dispatch(AgentEvent.NotificationReceived(params))
                }
            }
        }

        if (event != null) {
            eventAwaiter.notify(event)
        }

        if (event != null && AccessibilityEventPayloadFactory.shouldScheduleSemanticPublish(event.eventType)) {
            scheduleSemanticPublish(eventType = event.eventType)
        }
    }

    override fun onInterrupt() {
        Log.w(TAG, "Accessibility service interrupted")
        serviceScope.launch { accessibilityDisabledNotifier.notify("service_interrupted") }
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
        val runtimeBundle =
            ServiceRuntimeBootstrap(
                service = this,
                scope = serviceScope,
                logTag = TAG,
                snapshotProvider = snapshotProvider,
                eventAwaiter = eventAwaiter,
                awaitSettle = { awaitSettle() },
                runtimeHooks = ServiceRuntimeHooks(this, serviceScope, accessibilityDisabledNotifier, TAG),
                logger = ServiceAgentLogger(TAG),
            ).bootstrap()

        agentDeviceId = runtimeBundle.deviceId
        stateStore = runtimeBundle.stateStore
        outboundEventSeqNo.set(runtimeBundle.lastOutboundEventSeqNo)
        transport = runtimeBundle.transport
        coordinator = runtimeBundle.coordinator
        runtime = runtimeBundle.runtime
        runtimeBundle.runtime.start()

        serviceScope.launch {
            runtimeBundle.coordinator.dispatch(AgentEvent.ServiceConnected)
        }
    }

    private fun scheduleSemanticPublish(eventType: Int) {
        pendingScreenChangeJob?.cancel()
        pendingScreenChangeJob =
            serviceScope.launch {
                awaitSettle()
                val deviceId = agentDeviceId ?: return@launch
                val snapshot = snapshotProvider.build(deviceId) ?: return@launch
                val seqNo = outboundEventSeqNo.incrementAndGet()
                stateStore?.persistLastOutboundEventSeqNo(seqNo)
                val params =
                    AccessibilityEventPayloadFactory.buildScreenChangedParams(
                        seqNo = seqNo,
                        eventType = AccessibilityEventPayloadFactory.eventTypeName(eventType),
                        snapshot = snapshot,
                    )
                coordinator?.dispatch(AgentEvent.WindowStateChanged(params))
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

}
