package com.autosdk.agent.state

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentReducerTest {
    @Test
    fun `service connected activates service and requests socket connect`() {
        val reduction = AgentReducer.reduce(AgentState.initial(), AgentEvent.ServiceConnected)

        assertEquals(AgentServicePhase.ACTIVE, reduction.state.service)
        assertEquals(AgentTransportPhase.CONNECTING, reduction.state.transport)
        assertEquals(
            listOf(
                AgentEffect.Log("service_connected"),
                AgentEffect.ConnectSocket,
            ),
            reduction.effects,
        )
    }

    @Test
    fun `socket opened prefers resume when session id exists`() {
        val state =
            AgentState.initial().copy(
                service = AgentServicePhase.ACTIVE,
                transport = AgentTransportPhase.CONNECTING,
                sessionId = "session:device-001",
            )

        val reduction = AgentReducer.reduce(state, AgentEvent.SocketOpened)

        assertEquals(AgentTransportPhase.REGISTERING, reduction.state.transport)
        assertEquals(AgentRegistrationMode.RESUME, reduction.state.registrationMode)
        assertEquals(
            listOf(
                AgentEffect.SendResume("session:device-001"),
                AgentEffect.Log("socket_opened:resume"),
            ),
            reduction.effects,
        )
    }

    @Test
    fun `hello accepted moves transport to connected and persists session`() {
        val state =
            AgentState.initial().copy(
                service = AgentServicePhase.ACTIVE,
                transport = AgentTransportPhase.REGISTERING,
                registrationMode = AgentRegistrationMode.HELLO,
            )

        val reduction =
            AgentReducer.reduce(
                state,
                AgentEvent.HelloAccepted(
                    sessionId = "session:device-001",
                    atEpochMs = 1_700_000_000_000,
                ),
            )

        assertEquals(AgentTransportPhase.CONNECTED, reduction.state.transport)
        assertEquals("session:device-001", reduction.state.sessionId)
        assertEquals(0, reduction.state.reconnectAttempt)
        assertEquals(1_700_000_000_000, reduction.state.lastHeartbeatAtEpochMs)
        assertEquals(
            listOf(
                AgentEffect.PersistSessionId("session:device-001"),
                AgentEffect.StartHeartbeatTimer,
            ),
            reduction.effects,
        )
    }

    @Test
    fun `socket failure during execution abandons local execution and schedules backoff`() {
        val state =
            AgentState.initial().copy(
                service = AgentServicePhase.ACTIVE,
                transport = AgentTransportPhase.CONNECTED,
                execution = AgentExecutionPhase.EXECUTING_ACTION,
                currentRequestId = "req-1",
            )

        val reduction =
            AgentReducer.reduce(
                state,
                AgentEvent.SocketFailed("transport dropped"),
            )

        assertEquals(AgentTransportPhase.BACKOFF_WAIT, reduction.state.transport)
        assertEquals(AgentExecutionPhase.IDLE, reduction.state.execution)
        assertEquals(null, reduction.state.currentRequestId)
        assertEquals(1, reduction.state.reconnectAttempt)
        assertEquals("transport dropped", reduction.state.lastError)
        assertEquals(
            listOf(
                AgentEffect.StopHeartbeatTimer,
                AgentEffect.DisconnectSocket,
                AgentEffect.ClearInflightCommand,
                AgentEffect.ScheduleBackoff(1),
                AgentEffect.Log("command_abandoned:req-1:transport dropped"),
                AgentEffect.Log("socket_dropped:transport dropped"),
            ),
            reduction.effects,
        )
    }

    @Test
    fun `invalid transition logs and keeps state unchanged`() {
        val state = AgentState.initial()

        val reduction = AgentReducer.reduce(state, AgentEvent.SocketOpened)

        assertEquals(state, reduction.state)
        assertEquals(1, reduction.effects.size)
        assertTrue(reduction.effects.single() is AgentEffect.Log)
    }

    @Test
    fun `execute happy path reaches responding and returns to idle after response`() {
        val base =
            AgentState.initial().copy(
                service = AgentServicePhase.ACTIVE,
                transport = AgentTransportPhase.CONNECTED,
            )

        val afterExecute =
            AgentReducer.reduce(base, AgentEvent.ExecuteRequested("req-42")).state
        val afterBefore =
            AgentReducer.reduce(afterExecute, AgentEvent.SnapshotBeforeBuilt("req-42")).state
        val afterAction =
            AgentReducer.reduce(afterBefore, AgentEvent.ActionSucceeded("req-42")).state
        val afterSettle =
            AgentReducer.reduce(
                afterAction,
                AgentEvent.SettleCompleted("req-42", timedOut = false),
            ).state
        val afterSnapshot =
            AgentReducer.reduce(afterSettle, AgentEvent.SnapshotAfterBuilt("req-42")).state
        val finalState =
            AgentReducer.reduce(afterSnapshot, AgentEvent.ResponseSent("req-42")).state

        assertEquals(AgentExecutionPhase.RESPONDING, afterSnapshot.execution)
        assertEquals(AgentExecutionPhase.IDLE, finalState.execution)
        assertEquals(null, finalState.currentRequestId)
    }

    @Test
    fun `snapshot before failure moves execute flow to responding`() {
        val base =
            AgentState.initial().copy(
                service = AgentServicePhase.ACTIVE,
                transport = AgentTransportPhase.CONNECTED,
            )

        val afterExecute =
            AgentReducer.reduce(base, AgentEvent.ExecuteRequested("req-77")).state
        val afterFailure =
            AgentReducer.reduce(
                afterExecute,
                AgentEvent.SnapshotBeforeFailed("req-77", "Accessibility tree not available"),
            ).state

        assertEquals(AgentExecutionPhase.RESPONDING, afterFailure.execution)
        assertEquals("req-77", afterFailure.currentRequestId)
        assertEquals("Accessibility tree not available", afterFailure.lastError)
    }

    @Test
    fun `resume rejection clears session hint and falls back to backoff`() {
        val state =
            AgentState.initial().copy(
                service = AgentServicePhase.ACTIVE,
                transport = AgentTransportPhase.REGISTERING,
                registrationMode = AgentRegistrationMode.RESUME,
                sessionId = "session-stale",
            )

        val reduction =
            AgentReducer.reduce(
                state,
                AgentEvent.ResumeRejected("rpc_failure:agent.resume:-32003:stale"),
            )

        assertEquals(AgentTransportPhase.BACKOFF_WAIT, reduction.state.transport)
        assertEquals(null, reduction.state.sessionId)
        assertEquals(1, reduction.state.reconnectAttempt)
        assertEquals(
            listOf(
                AgentEffect.ClearSessionId,
                AgentEffect.DisconnectSocket,
                AgentEffect.ScheduleBackoff(1),
                AgentEffect.Log("resume_rejected:rpc_failure:agent.resume:-32003:stale"),
            ),
            reduction.effects,
        )
    }
}
