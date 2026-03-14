package com.autosdk.agent.state

import android.content.Context
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment

@RunWith(RobolectricTestRunner::class)
class AgentStateStoreTest {
    private val appContext = RuntimeEnvironment.getApplication()
    private val store = SharedPreferencesAgentStateStore.from(appContext)

    @Before
    fun resetStore() {
        appContext.getSharedPreferences("agent_state", Context.MODE_PRIVATE)
            .edit()
            .clear()
            .commit()
    }

    @Test
    fun `persists outbound event seq and pending accessibility event`() {
        store.persistLastOutboundEventSeqNo(17L)
        store.persistPendingAccessibilityDisabledEvent(
            PendingAccessibilityDisabledEvent(
                seqNo = 17L,
                reason = "service_interrupted",
            ),
        )

        val persisted = store.read()
        assertEquals(17L, persisted.lastOutboundEventSeqNo)
        assertEquals(17L, persisted.pendingAccessibilityDisabledEvent?.seqNo)
        assertEquals("service_interrupted", persisted.pendingAccessibilityDisabledEvent?.reason)
    }

    @Test
    fun `clears pending accessibility event without resetting outbound seq`() {
        store.persistLastOutboundEventSeqNo(23L)
        store.persistPendingAccessibilityDisabledEvent(
            PendingAccessibilityDisabledEvent(
                seqNo = 23L,
                reason = "service_interrupted",
            ),
        )

        store.clearPendingAccessibilityDisabledEvent()

        val persisted = store.read()
        assertEquals(23L, persisted.lastOutboundEventSeqNo)
        assertNull(persisted.pendingAccessibilityDisabledEvent)
    }
}

