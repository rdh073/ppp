package com.autosdk.agent.state

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import com.autosdk.agent.agent.AgentCapabilities
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import java.util.concurrent.atomic.AtomicLong

private const val METHOD_AGENT_HELLO = "agent.hello"
private const val METHOD_AGENT_RESUME = "agent.resume"
private const val METHOD_AGENT_HEARTBEAT = "agent.heartbeat"
private const val METHOD_AGENT_DISCONNECT = "agent.disconnect"
private const val BACKOFF_EXHAUSTION_WARNING_ATTEMPT = 5

fun interface AgentLogger {
    fun log(message: String)
}

interface AgentTransportDriver {
    suspend fun connect()

    fun disconnect()

    suspend fun sendRequest(
        id: String,
        method: String,
        params: JsonElement,
    )

    fun onSocketOpened(handler: () -> Unit)

    fun onSocketClosed(handler: (String) -> Unit)

    fun onSocketFailure(handler: (String) -> Unit)

    fun onRpcSuccess(handler: (String, JsonElement) -> Unit)

    fun onRpcFailure(handler: (String, Int, String) -> Unit)

    suspend fun sendNotification(method: String, params: JsonElement): Boolean = false
}

interface AgentHeartbeatScheduler {
    fun start()

    fun stop()
}

interface AgentBackoffScheduler {
    fun schedule(attempt: Int)
}

interface AgentRuntimeHooks {
    fun clearInflightCommand()

    fun onStatusChanged(status: AgentStatus) {}
}

class CoroutineHeartbeatScheduler(
    private val scope: CoroutineScope,
    private val intervalMs: Long = 30_000L,
    private val onTick: suspend () -> Unit,
) : AgentHeartbeatScheduler {
    private var job: Job? = null

    override fun start() {
        if (job?.isActive == true) {
            return
        }

        job =
            scope.launch {
                while (isActive) {
                    delay(intervalMs)
                    if (!isActive) {
                        return@launch
                    }
                    onTick()
                }
            }
    }

    override fun stop() {
        job?.cancel()
        job = null
    }
}

class CoroutineBackoffScheduler(
    private val scope: CoroutineScope,
    private val baseDelayMs: Long = 2_000L,
    private val maxDelayMs: Long = 60_000L,
    private val onElapsed: suspend () -> Unit,
) : AgentBackoffScheduler {
    private var job: Job? = null

    override fun schedule(attempt: Int) {
        job?.cancel()
        job =
            scope.launch {
                delay(computeDelayMs(attempt))
                if (!isActive) {
                    return@launch
                }
                onElapsed()
            }
    }

    private fun computeDelayMs(attempt: Int): Long {
        if (attempt <= 0) {
            return 0L
        }

        val shift = (attempt - 1).coerceAtMost(5)
        return minOf(baseDelayMs shl shift, maxDelayMs)
    }
}

class AgentStateCoordinator(
    private val scope: CoroutineScope,
    private val deviceId: String,
    private val store: AgentStateStore,
    private val transportDriver: AgentTransportDriver,
    private val heartbeatScheduler: AgentHeartbeatScheduler,
    private val backoffScheduler: AgentBackoffScheduler,
    private val runtimeHooks: AgentRuntimeHooks,
    private val logger: AgentLogger,
    private val capabilitiesProvider: () -> List<Map<String, Any>> = { emptyList() },
    private val clock: () -> Long = { System.currentTimeMillis() },
) {
    private val mutex = Mutex()
    private val outboundRequests = mutableMapOf<String, String>()
    private val requestCounter = AtomicLong(0L)
    private val persistedState = store.read()
    private val agentInstanceId = persistedState.agentInstanceId

    @Volatile
    private var state =
        AgentState.initial().copy(
            sessionId = persistedState.sessionId,
            lastRegistrationAtEpochMs = persistedState.lastRegistrationAtEpochMs,
        )

    init {
        bindTransportCallbacks()
        runtimeHooks.onStatusChanged(state.toStatus())
    }

    fun currentState(): AgentState = state

    fun currentStatus(): AgentStatus = state.toStatus()

    fun dumpStatus(): String = currentStatus().toDebugString()

    suspend fun dispatch(event: AgentEvent): AgentState {
        val reduction =
            mutex.withLock {
                val previousState = state
                val nextReduction = AgentReducer.reduce(previousState, event)
                state = nextReduction.state
                syncStore(previousState, nextReduction.state)
                clearPendingRequestsIfNeeded(event)
                runtimeHooks.onStatusChanged(nextReduction.state.toStatus())
                nextReduction
            }

        reduction.effects.forEach { effect -> runEffect(effect, reduction.state) }
        return reduction.state
    }

    private fun bindTransportCallbacks() {
        transportDriver.onSocketOpened {
            scope.launch { dispatch(AgentEvent.SocketOpened) }
        }
        transportDriver.onSocketClosed { reason ->
            scope.launch { dispatch(AgentEvent.SocketClosed(reason)) }
        }
        transportDriver.onSocketFailure { reason ->
            scope.launch { dispatch(AgentEvent.SocketFailed(reason)) }
        }
        transportDriver.onRpcSuccess { requestId, result ->
            scope.launch { onRpcSuccess(requestId, result) }
        }
        transportDriver.onRpcFailure { requestId, code, message ->
            scope.launch { onRpcFailure(requestId, code, message) }
        }
    }

    private fun syncStore(
        previousState: AgentState,
        nextState: AgentState,
    ) {
        if (previousState.currentRequestId != nextState.currentRequestId) {
            if (nextState.currentRequestId != null) {
                store.persistInflightRequestId(nextState.currentRequestId)
            } else {
                store.clearInflightRequestId()
            }
        }
    }

    private fun clearPendingRequestsIfNeeded(event: AgentEvent) {
        when (event) {
            AgentEvent.BootCompleted,
            AgentEvent.PackageReplaced,
            AgentEvent.ServiceInterrupted,
            AgentEvent.ServiceDestroyed,
            is AgentEvent.SocketClosed,
            is AgentEvent.SocketFailed,
            is AgentEvent.SocketOpenFailed -> outboundRequests.clear()
            else -> Unit
        }
    }

    /**
     * Executes a single side-effect produced by [AgentReducer].
     *
     * Effects fall into two categories:
     *
     * **Tracked effects** — SendHello, SendResume, SendHeartbeat:
     * Use [sendAgentRequest], which registers the outbound request by ID.
     * The corresponding [onRpcSuccess] / [onRpcFailure] callbacks handle the
     * outcome and dispatch the appropriate [AgentEvent] back into the state machine.
     *
     * **Fire-and-forget effects** — SendDisconnect, PublishUiEvent, ConnectSocket:
     * Wrap in `runCatching { ... }.onFailure { log }`. Failures are logged but
     * do not trigger state transitions; they are inherently best-effort.
     */
    private suspend fun runEffect(
        effect: AgentEffect,
        currentState: AgentState,
    ) {
        when (effect) {
            AgentEffect.ConnectSocket ->
                runCatching {
                    transportDriver.connect()
                }.onFailure { error ->
                    dispatch(
                        AgentEvent.SocketOpenFailed(
                            error.message ?: "socket_open_failed",
                        ),
                    )
                }

            AgentEffect.DisconnectSocket -> transportDriver.disconnect()
            AgentEffect.SendHello ->
                sendAgentRequest(
                    method = METHOD_AGENT_HELLO,
                    params =
                        buildJsonObject {
                            put("deviceId", deviceId)
                            put("agentInstanceId", agentInstanceId)
                            put("capabilities", capabilitiesToJson())
                        },
                )

            is AgentEffect.SendResume ->
                sendAgentRequest(
                    method = METHOD_AGENT_RESUME,
                    params =
                        buildJsonObject {
                            put("deviceId", deviceId)
                            put("sessionId", effect.sessionId)
                            put("capabilities", capabilitiesToJson())
                        },
                )

            is AgentEffect.SendDisconnect ->
                runCatching {
                    sendAgentRequest(
                        method = METHOD_AGENT_DISCONNECT,
                        params =
                            buildJsonObject {
                                put("deviceId", deviceId)
                                put("sessionId", effect.sessionId)
                            },
                    )
                }.onFailure { error ->
                    logger.log("disconnect_send_failed:${error.message ?: "unknown"}")
                }

            AgentEffect.SendHeartbeat ->
                sendAgentRequest(
                    method = METHOD_AGENT_HEARTBEAT,
                    params =
                        buildJsonObject {
                            put("deviceId", deviceId)
                            currentState.sessionId?.let { sessionId ->
                                put("sessionId", sessionId)
                            }
                        },
                )

            AgentEffect.StartHeartbeatTimer -> heartbeatScheduler.start()
            AgentEffect.StopHeartbeatTimer -> heartbeatScheduler.stop()
            is AgentEffect.ScheduleBackoff -> {
                if (effect.attempt >= BACKOFF_EXHAUSTION_WARNING_ATTEMPT) {
                    logger.log("backoff_exhaustion_warning:attempt=${effect.attempt}")
                }
                backoffScheduler.schedule(effect.attempt)
            }
            is AgentEffect.PersistSessionId ->
                store.persistSession(
                    sessionId = effect.sessionId,
                    lastRegistrationAtEpochMs = currentState.lastRegistrationAtEpochMs,
                )

            AgentEffect.ClearSessionId -> store.clearSession()

            AgentEffect.ClearInflightCommand -> {
                store.clearInflightRequestId()
                runtimeHooks.clearInflightCommand()
            }

            is AgentEffect.Log -> logger.log(effect.message)

            is AgentEffect.PublishUiEvent ->
                runCatching {
                    transportDriver.sendNotification(effect.method, effect.params)
                }.onFailure { error ->
                    logger.log("publish_ui_event_failed:${effect.method}:${error.message}")
                }.onSuccess { sent ->
                    if (!sent) {
                        logger.log("publish_ui_event_failed:${effect.method}:socket_unavailable")
                    }
                }
        }
    }

    private suspend fun sendAgentRequest(
        method: String,
        params: JsonElement,
    ) {
        val requestId = nextRequestId(method)
        outboundRequests[requestId] = method
        transportDriver.sendRequest(
            id = requestId,
            method = method,
            params = params,
        )
    }

    private suspend fun onRpcSuccess(
        requestId: String,
        result: JsonElement,
    ) {
        val method = outboundRequests.remove(requestId) ?: run {
            logger.log("unknown_rpc_success:$requestId")
            return
        }

        val accepted =
            result.jsonObject["accepted"]?.jsonPrimitive?.contentOrNull?.toBooleanStrictOrNull()
                ?: true

        if (!accepted) {
            onRpcFailure(requestId, -32003, "rpc rejected: $method")
            return
        }

        when (method) {
            METHOD_AGENT_HELLO -> {
                val sessionId = result.sessionId() ?: run {
                    logger.log("missing_session_id:$method")
                    return
                }
                dispatch(AgentEvent.HelloAccepted(sessionId, clock()))
            }

            METHOD_AGENT_RESUME -> {
                val sessionId = result.sessionId() ?: run {
                    dispatch(AgentEvent.ResumeRejected("missing_session_id:$method"))
                    return
                }
                dispatch(AgentEvent.ResumeAccepted(sessionId, clock()))
            }

            METHOD_AGENT_HEARTBEAT ->
                dispatch(
                    AgentEvent.HeartbeatAccepted(clock()),
                )

            METHOD_AGENT_DISCONNECT ->
                logger.log("disconnect_ack:$requestId")
        }
    }

    private suspend fun onRpcFailure(
        requestId: String,
        code: Int,
        message: String,
    ) {
        val method = outboundRequests.remove(requestId) ?: run {
            logger.log("unknown_rpc_failure:$requestId:$code:$message")
            return
        }

        when (method) {
            METHOD_AGENT_HELLO,
            METHOD_AGENT_HEARTBEAT ->
                dispatch(
                    AgentEvent.SocketFailed("rpc_failure:$method:$code:$message"),
                )

            METHOD_AGENT_RESUME ->
                dispatch(
                    AgentEvent.ResumeRejected("rpc_failure:$method:$code:$message"),
                )

            METHOD_AGENT_DISCONNECT ->
                logger.log("disconnect_failure:$code:$message")
        }
    }

    private fun nextRequestId(method: String): String =
        "$method:${requestCounter.incrementAndGet()}"

    private fun capabilitiesToJson(): JsonElement =
        AgentCapabilities.capabilitiesToJson(capabilitiesProvider())
}

private fun JsonElement.sessionId(): String? =
    jsonObject["sessionId"]?.jsonPrimitive?.contentOrNull
