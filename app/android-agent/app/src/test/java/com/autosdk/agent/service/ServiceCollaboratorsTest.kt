package com.autosdk.agent.service

import com.autosdk.agent.state.AgentStatus
import com.autosdk.agent.state.AgentStateStore
import com.autosdk.agent.state.AgentTransportDriver
import com.autosdk.agent.state.PendingAccessibilityDisabledEvent
import com.autosdk.agent.state.PersistedAgentState
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancel
import kotlinx.serialization.json.JsonElement
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment

/**
 * Verifies that [ServiceRuntimeHooks] and [ServiceAgentLogger] can be
 * instantiated and exercised independently of [AgentAccessibilityService].
 */
@RunWith(RobolectricTestRunner::class)
class ServiceCollaboratorsTest {

    private val context = RuntimeEnvironment.getApplication()

    // ── ServiceAgentLogger ───────────────────────────────────────────────────

    @Test
    fun `ServiceAgentLogger log does not throw`() {
        val logger = ServiceAgentLogger(tag = "test")
        logger.log("hello from test")
    }

    // ── ServiceRuntimeHooks ──────────────────────────────────────────────────

    @Test
    fun `ServiceRuntimeHooks onStatusChanged does not throw when transport connects`() {
        val notifier = fakeNotifier()
        val scope = CoroutineScope(Dispatchers.Unconfined)
        val hooks = ServiceRuntimeHooks(context, scope, notifier, "test")

        hooks.onStatusChanged(statusWith("disconnected"))
        hooks.onStatusChanged(statusWith("connected"))

        scope.cancel()
    }

    @Test
    fun `ServiceRuntimeHooks can be used without AgentAccessibilityService`() {
        val notifier = fakeNotifier()
        val scope = CoroutineScope(Dispatchers.Unconfined)

        // This proves extraction is complete — no reference to the service needed.
        val hooks = ServiceRuntimeHooks(context, scope, notifier, "test")
        hooks.onStatusChanged(statusWith("connected"))
        hooks.onStatusChanged(statusWith("connected"))

        scope.cancel()
    }

    // ── Helpers ──────────────────────────────────────────────────────────────

    private fun statusWith(transport: String) = AgentStatus(
        service = "running",
        transport = transport,
        execution = "idle",
        registrationMode = null,
        reconnectAttempt = 0,
        sessionId = null,
        lastHeartbeatAtEpochMs = null,
        lastRegistrationAtEpochMs = null,
        currentRequestId = null,
        lastError = null,
    )

    private fun fakeNotifier(): AccessibilityDisabledNotifier {
        val store = FakeStateStore()
        val transport = FakeTransport()
        return AccessibilityDisabledNotifier(
            logTag = "test",
            stateStoreProvider = { store },
            transportDriverProvider = { transport },
            nextSeqNo = { 1L },
        )
    }
}

// ── Fakes ────────────────────────────────────────────────────────────────────

private class FakeStateStore : AgentStateStore {
    override fun read() = PersistedAgentState(
        agentInstanceId = "test-agent",
        sessionId = null,
        lastRegistrationAtEpochMs = null,
        inflightRequestId = null,
        lastOutboundEventSeqNo = 0L,
        pendingAccessibilityDisabledEvent = null,
    )

    override fun persistSession(sessionId: String, lastRegistrationAtEpochMs: Long?) = Unit
    override fun clearSession() = Unit
    override fun persistInflightRequestId(requestId: String) = Unit
    override fun clearInflightRequestId() = Unit
    override fun persistLastOutboundEventSeqNo(seqNo: Long) = Unit
    override fun persistPendingAccessibilityDisabledEvent(event: PendingAccessibilityDisabledEvent) = Unit
    override fun clearPendingAccessibilityDisabledEvent() = Unit
}

private class FakeTransport : AgentTransportDriver {
    override suspend fun connect() = Unit
    override fun disconnect() = Unit
    override suspend fun sendRequest(id: String, method: String, params: JsonElement) = Unit
    override fun onSocketOpened(handler: () -> Unit) = Unit
    override fun onSocketClosed(handler: (String) -> Unit) = Unit
    override fun onSocketFailure(handler: (String) -> Unit) = Unit
    override fun onRpcSuccess(handler: (String, JsonElement) -> Unit) = Unit
    override fun onRpcFailure(handler: (String, Int, String) -> Unit) = Unit
    override suspend fun sendNotification(method: String, params: JsonElement): Boolean = true
}
