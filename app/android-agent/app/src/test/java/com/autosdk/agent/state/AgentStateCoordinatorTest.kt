package com.autosdk.agent.state

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class AgentStateCoordinatorTest {
    @Test
    fun `persisted session prefers resume after socket open`() =
        runBlocking {
            val fixture =
                CoordinatorFixture(
                    store =
                        FakeAgentStateStore(
                            PersistedAgentState(
                                agentInstanceId = "agent-1",
                                sessionId = "session-1",
                                lastRegistrationAtEpochMs = 10L,
                                inflightRequestId = null,
                            ),
                        ),
                )

            fixture.coordinator.dispatch(AgentEvent.ServiceConnected)
            fixture.transport.emitSocketOpened()
            fixture.flush()

            assertEquals(1, fixture.transport.connectCalls)
            assertEquals("agent.resume", fixture.transport.sentRequests.single().method)

            val request = fixture.transport.sentRequests.single()
            fixture.transport.emitRpcSuccess(
                request.id,
                buildJsonObject {
                    put("accepted", true)
                    put("sessionId", "session-1")
                },
            )
            fixture.flush()

            assertEquals(1, fixture.heartbeatScheduler.startCalls)
            assertEquals("session-1", fixture.store.snapshot.sessionId)
            assertEquals(AgentTransportPhase.CONNECTED, fixture.coordinator.currentState().transport)
        }

    @Test
    fun `missing persisted session falls back to hello`() =
        runBlocking {
            val fixture = CoordinatorFixture()

            fixture.coordinator.dispatch(AgentEvent.ServiceConnected)
            fixture.transport.emitSocketOpened()
            fixture.flush()

            val request = fixture.transport.sentRequests.single()
            assertEquals("agent.hello", request.method)
            assertEquals(
                "observe",
                request.params.jsonObject["capabilities"]!!
                    .jsonArray
                    .first()
                    .jsonObject["name"]!!
                    .jsonPrimitive
                    .content,
            )
            assertEquals(
                "Pixel 7",
                request.params.jsonObject["deviceMetadata"]!!
                    .jsonObject["model"]!!
                    .jsonPrimitive
                    .content,
            )
        }

    @Test
    fun `boot completed clears inflight marker`() =
        runBlocking {
            val fixture = CoordinatorFixture()

            fixture.coordinator.dispatch(AgentEvent.ServiceConnected)
            fixture.coordinator.dispatch(AgentEvent.ExecuteRequested("req-1"))

            assertEquals("req-1", fixture.store.snapshot.inflightRequestId)

            fixture.coordinator.dispatch(AgentEvent.BootCompleted)

            assertNull(fixture.store.snapshot.inflightRequestId)
            assertEquals(1, fixture.runtimeHooks.clearInflightCalls)
            assertEquals(AgentExecutionPhase.IDLE, fixture.coordinator.currentState().execution)
        }

    @Test
    fun `package replaced clears inflight marker`() =
        runBlocking {
            val fixture = CoordinatorFixture()

            fixture.coordinator.dispatch(AgentEvent.ServiceConnected)
            fixture.coordinator.dispatch(AgentEvent.ExecuteRequested("req-2"))

            assertEquals("req-2", fixture.store.snapshot.inflightRequestId)

            fixture.coordinator.dispatch(AgentEvent.PackageReplaced)

            assertNull(fixture.store.snapshot.inflightRequestId)
            assertEquals(1, fixture.runtimeHooks.clearInflightCalls)
            assertEquals(AgentServicePhase.AWAITING_SERVICE, fixture.coordinator.currentState().service)
        }

    @Test
    fun `connected heartbeat sends request and updates heartbeat timestamp`() =
        runBlocking {
            val fixture = CoordinatorFixture()

            fixture.coordinator.dispatch(AgentEvent.ServiceConnected)
            fixture.transport.emitSocketOpened()
            fixture.flush()

            val helloRequest = fixture.transport.sentRequests.single()
            fixture.transport.emitRpcSuccess(
                helloRequest.id,
                buildJsonObject {
                    put("accepted", true)
                    put("sessionId", "session-1")
                },
            )
            fixture.flush()

            fixture.coordinator.dispatch(AgentEvent.HeartbeatTick)
            fixture.flush()

            val heartbeatRequest = fixture.transport.sentRequests.last()
            assertEquals("agent.heartbeat", heartbeatRequest.method)

            fixture.transport.emitRpcSuccess(
                heartbeatRequest.id,
                buildJsonObject {
                    put("accepted", true)
                },
            )
            fixture.flush()

            assertEquals(1, fixture.heartbeatScheduler.startCalls)
            assertEquals(1_700_000_000_000L, fixture.coordinator.currentState().lastHeartbeatAtEpochMs)
            assertEquals(
                "service=active transport=connected execution=idle reconnectAttempt=0 sessionId=session-1 lastHeartbeatAt=1700000000000 lastRegistrationAt=1700000000000",
                fixture.coordinator.dumpStatus(),
            )
        }

    @Test
    fun `stale resume hint is cleared and next registration falls back to hello`() =
        runBlocking {
            val fixture =
                CoordinatorFixture(
                    store =
                        FakeAgentStateStore(
                            PersistedAgentState(
                                agentInstanceId = "agent-1",
                                sessionId = "session-stale",
                                lastRegistrationAtEpochMs = 10L,
                                inflightRequestId = null,
                            ),
                        ),
                )

            fixture.coordinator.dispatch(AgentEvent.ServiceConnected)
            fixture.transport.emitSocketOpened()
            fixture.flush()

            val resumeRequest = fixture.transport.sentRequests.single()
            assertEquals("agent.resume", resumeRequest.method)

            fixture.transport.emitRpcFailure(
                resumeRequest.id,
                -32003,
                "stale session",
            )
            fixture.flush()
            fixture.transport.emitSocketClosed("Agent shutting down")
            fixture.flush()

            assertNull(fixture.store.snapshot.sessionId)
            assertEquals(1, fixture.backoffScheduler.scheduledAttempts.single())
            assertEquals(0, fixture.logs.count { it.startsWith("invalid_transition:SocketFailed") })
            assertEquals(0, fixture.logs.count { it.startsWith("invalid_transition:SocketClosed") })

            fixture.coordinator.dispatch(AgentEvent.BackoffElapsed)
            fixture.transport.emitSocketOpened()
            fixture.flush()

            assertEquals("agent.hello", fixture.transport.sentRequests.last().method)
        }

    @Test
    fun `reconnect after accepted session uses resume path`() =
        runBlocking {
            val fixture = CoordinatorFixture()

            fixture.coordinator.dispatch(AgentEvent.ServiceConnected)
            fixture.transport.emitSocketOpened()
            fixture.flush()

            val helloRequest = fixture.transport.sentRequests.single()
            fixture.transport.emitRpcSuccess(
                helloRequest.id,
                buildJsonObject {
                    put("accepted", true)
                    put("sessionId", "session-2")
                },
            )
            fixture.flush()

            fixture.transport.emitSocketFailure("network dropped")
            fixture.flush()

            fixture.coordinator.dispatch(AgentEvent.BackoffElapsed)
            fixture.transport.emitSocketOpened()
            fixture.flush()

            assertEquals("agent.resume", fixture.transport.sentRequests.last().method)
        }
}

private class CoordinatorFixture(
    val store: FakeAgentStateStore =
        FakeAgentStateStore(
            PersistedAgentState(
                agentInstanceId = "agent-1",
                sessionId = null,
                lastRegistrationAtEpochMs = null,
                inflightRequestId = null,
            ),
        ),
) {
    private val scope = CoroutineScope(Dispatchers.Unconfined + Job())
    val logs = mutableListOf<String>()
    val transport = FakeTransportDriver()
    val heartbeatScheduler = FakeHeartbeatScheduler()
    val backoffScheduler = FakeBackoffScheduler()
    val runtimeHooks = FakeRuntimeHooks()
    val coordinator =
        AgentStateCoordinator(
            scope = scope,
            deviceId = "device-1",
            store = store,
            transportDriver = transport,
            heartbeatScheduler = heartbeatScheduler,
            backoffScheduler = backoffScheduler,
            runtimeHooks = runtimeHooks,
            logger = AgentLogger { message -> logs += message },
            capabilitiesProvider = {
                listOf(
                    mapOf("name" to "observe", "available" to true),
                    mapOf(
                        "name" to "screenshot",
                        "available" to false,
                        "reason" to "Requires API 30+",
                    ),
                )
            },
            deviceMetadataProvider = {
                mapOf(
                    "manufacturer" to "Google",
                    "model" to "Pixel 7",
                    "androidVersion" to "15",
                    "sdkInt" to 35,
                )
            },
            clock = { 1_700_000_000_000L },
        )

    suspend fun flush() {
        repeat(2) {
            delay(1)
        }
    }
}

private class FakeAgentStateStore(
    initial: PersistedAgentState,
) : AgentStateStore {
    var snapshot = initial
        private set

    override fun read(): PersistedAgentState = snapshot

    override fun persistSession(
        sessionId: String,
        lastRegistrationAtEpochMs: Long?,
    ) {
        snapshot =
            snapshot.copy(
                sessionId = sessionId,
                lastRegistrationAtEpochMs = lastRegistrationAtEpochMs,
            )
    }

    override fun clearSession() {
        snapshot =
            snapshot.copy(
                sessionId = null,
                lastRegistrationAtEpochMs = null,
            )
    }

    override fun persistInflightRequestId(requestId: String) {
        snapshot = snapshot.copy(inflightRequestId = requestId)
    }

    override fun clearInflightRequestId() {
        snapshot = snapshot.copy(inflightRequestId = null)
    }

    override fun persistLastOutboundEventSeqNo(seqNo: Long) {
        snapshot = snapshot.copy(lastOutboundEventSeqNo = seqNo)
    }

    override fun persistPendingAccessibilityDisabledEvent(event: PendingAccessibilityDisabledEvent) {
        snapshot = snapshot.copy(pendingAccessibilityDisabledEvent = event)
    }

    override fun clearPendingAccessibilityDisabledEvent() {
        snapshot = snapshot.copy(pendingAccessibilityDisabledEvent = null)
    }
}

private class FakeTransportDriver : AgentTransportDriver {
    data class SentRequest(
        val id: String,
        val method: String,
        val params: JsonElement,
    )

    var connectCalls = 0
        private set
    var disconnectCalls = 0
        private set
    val sentRequests = mutableListOf<SentRequest>()

    private var socketOpenedHandler: (() -> Unit)? = null
    private var socketClosedHandler: ((String) -> Unit)? = null
    private var socketFailureHandler: ((String) -> Unit)? = null
    private var rpcSuccessHandler: ((String, JsonElement) -> Unit)? = null
    private var rpcFailureHandler: ((String, Int, String) -> Unit)? = null

    override suspend fun connect() {
        connectCalls += 1
    }

    override fun disconnect() {
        disconnectCalls += 1
    }

    override suspend fun sendRequest(
        id: String,
        method: String,
        params: JsonElement,
    ) {
        sentRequests += SentRequest(id = id, method = method, params = params)
    }

    override fun onSocketOpened(handler: () -> Unit) {
        socketOpenedHandler = handler
    }

    override fun onSocketClosed(handler: (String) -> Unit) {
        socketClosedHandler = handler
    }

    override fun onSocketFailure(handler: (String) -> Unit) {
        socketFailureHandler = handler
    }

    override fun onRpcSuccess(handler: (String, JsonElement) -> Unit) {
        rpcSuccessHandler = handler
    }

    override fun onRpcFailure(handler: (String, Int, String) -> Unit) {
        rpcFailureHandler = handler
    }

    fun emitSocketOpened() {
        socketOpenedHandler?.invoke()
    }

    fun emitSocketClosed(reason: String) {
        socketClosedHandler?.invoke(reason)
    }

    fun emitSocketFailure(reason: String) {
        socketFailureHandler?.invoke(reason)
    }

    fun emitRpcSuccess(
        requestId: String,
        result: JsonElement,
    ) {
        rpcSuccessHandler?.invoke(requestId, result)
    }

    fun emitRpcFailure(
        requestId: String,
        code: Int,
        message: String,
    ) {
        rpcFailureHandler?.invoke(requestId, code, message)
    }
}

private class FakeHeartbeatScheduler : AgentHeartbeatScheduler {
    var startCalls = 0
        private set
    var stopCalls = 0
        private set

    override fun start() {
        startCalls += 1
    }

    override fun stop() {
        stopCalls += 1
    }
}

private class FakeBackoffScheduler : AgentBackoffScheduler {
    val scheduledAttempts = mutableListOf<Int>()

    override fun schedule(attempt: Int) {
        scheduledAttempts += attempt
    }
}

private class FakeRuntimeHooks : AgentRuntimeHooks {
    var clearInflightCalls = 0
        private set

    override fun clearInflightCommand() {
        clearInflightCalls += 1
    }
}
