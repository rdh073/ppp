package com.autosdk.agent.boot

import android.content.Intent
import com.autosdk.agent.state.SharedPreferencesAgentStateStore
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment

@RunWith(RobolectricTestRunner::class)
class BootCompletedReceiverTest {
    private val appContext = RuntimeEnvironment.getApplication()
    private val store = SharedPreferencesAgentStateStore.from(appContext)
    private val receiver = BootCompletedReceiver()

    @Before
    fun resetStore() {
        appContext.getSharedPreferences("agent_state", android.content.Context.MODE_PRIVATE)
            .edit()
            .clear()
            .commit()
    }

    @Test
    fun `boot completed clears inflight marker but preserves resume hint`() {
        store.persistSession(
            sessionId = "session-boot",
            lastRegistrationAtEpochMs = 1_700_000_000_000L,
        )
        store.persistInflightRequestId("req-boot")

        receiver.onReceive(appContext, Intent(Intent.ACTION_BOOT_COMPLETED))

        val persisted = store.read()
        assertEquals("session-boot", persisted.sessionId)
        assertEquals(1_700_000_000_000L, persisted.lastRegistrationAtEpochMs)
        assertNull(persisted.inflightRequestId)
    }

    @Test
    fun `package replaced clears inflight marker but preserves resume hint`() {
        store.persistSession(
            sessionId = "session-replace",
            lastRegistrationAtEpochMs = 1_700_000_000_123L,
        )
        store.persistInflightRequestId("req-replace")

        receiver.onReceive(appContext, Intent(Intent.ACTION_MY_PACKAGE_REPLACED))

        val persisted = store.read()
        assertEquals("session-replace", persisted.sessionId)
        assertEquals(1_700_000_000_123L, persisted.lastRegistrationAtEpochMs)
        assertNull(persisted.inflightRequestId)
    }

    @Test
    fun `boot completed creates stable agent instance id when store empty`() {
        receiver.onReceive(appContext, Intent(Intent.ACTION_BOOT_COMPLETED))

        val first = store.read().agentInstanceId
        val second = store.read().agentInstanceId

        assertEquals(first, second)
    }
}
