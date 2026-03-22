package com.autosdk.agent.service

import com.autosdk.agent.state.AgentStateStore
import com.autosdk.agent.state.AgentTransportDriver
import com.autosdk.agent.state.PendingAccessibilityDisabledEvent
import com.autosdk.agent.state.PersistedAgentState
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertNotNull
import org.junit.Test

class AccessibilityDisabledNotifierTest {
    @Test
    fun `notify stores and clears pending event when transport sends successfully`() = runBlocking {
        val store = FakeAgentStateStore()
        val transport = FakeTransportDriver(sendResult = true)
        val notifier =
            AccessibilityDisabledNotifier(
                logTag = "test",
                stateStoreProvider = { store },
                transportDriverProvider = { transport },
                nextSeqNo = { 7L },
            )

        notifier.notify("service_interrupted")

        assertEquals(7L, store.lastOutboundSeqNo)
        assertNull(store.pendingEvent)
        assertEquals("android.accessibility.disabled", transport.lastMethod)
        assertEquals("service_interrupted", transport.lastParams?.jsonObject?.get("reason")?.jsonPrimitive?.content)
    }

    @Test
    fun `flushPending keeps pending event when transport send fails`() = runBlocking {
        val store = FakeAgentStateStore()
        store.pendingEvent = PendingAccessibilityDisabledEvent(seqNo = 11L, reason = "manual_disable")
        val transport = FakeTransportDriver(sendResult = false)
        val notifier =
            AccessibilityDisabledNotifier(
                logTag = "test",
                stateStoreProvider = { store },
                transportDriverProvider = { transport },
                nextSeqNo = { 99L },
            )

        notifier.flushPending()

        assertNotNull(store.pendingEvent)
        assertEquals(11L, store.pendingEvent?.seqNo)
        assertEquals("android.accessibility.disabled", transport.lastMethod)
    }
}

private class FakeAgentStateStore : AgentStateStore {
    var sessionId: String? = null
    var lastRegistrationAtEpochMs: Long? = null
    var inflightRequestId: String? = null
    var lastOutboundSeqNo: Long = 0L
    var pendingEvent: PendingAccessibilityDisabledEvent? = null

    override fun read(): PersistedAgentState =
        PersistedAgentState(
            agentInstanceId = "agent-instance",
            sessionId = sessionId,
            lastRegistrationAtEpochMs = lastRegistrationAtEpochMs,
            inflightRequestId = inflightRequestId,
            lastOutboundEventSeqNo = lastOutboundSeqNo,
            pendingAccessibilityDisabledEvent = pendingEvent,
        )

    override fun persistSession(sessionId: String, lastRegistrationAtEpochMs: Long?) {
        this.sessionId = sessionId
        this.lastRegistrationAtEpochMs = lastRegistrationAtEpochMs
    }

    override fun clearSession() {
        sessionId = null
        lastRegistrationAtEpochMs = null
    }

    override fun persistInflightRequestId(requestId: String) {
        inflightRequestId = requestId
    }

    override fun clearInflightRequestId() {
        inflightRequestId = null
    }

    override fun persistLastOutboundEventSeqNo(seqNo: Long) {
        lastOutboundSeqNo = seqNo
    }

    override fun persistPendingAccessibilityDisabledEvent(event: PendingAccessibilityDisabledEvent) {
        pendingEvent = event
    }

    override fun clearPendingAccessibilityDisabledEvent() {
        pendingEvent = null
    }
}

private class FakeTransportDriver(
    private val sendResult: Boolean,
) : AgentTransportDriver {
    var lastMethod: String? = null
    var lastParams: JsonElement? = null

    override suspend fun connect() = Unit

    override fun disconnect() = Unit

    override suspend fun sendRequest(id: String, method: String, params: JsonElement) = Unit

    override fun onSocketOpened(handler: () -> Unit) = Unit

    override fun onSocketClosed(handler: (String) -> Unit) = Unit

    override fun onSocketFailure(handler: (String) -> Unit) = Unit

    override fun onRpcSuccess(handler: (String, JsonElement) -> Unit) = Unit

    override fun onRpcFailure(handler: (String, Int, String) -> Unit) = Unit

    override suspend fun sendNotification(method: String, params: JsonElement): Boolean {
        lastMethod = method
        lastParams = params
        return sendResult
    }
}
