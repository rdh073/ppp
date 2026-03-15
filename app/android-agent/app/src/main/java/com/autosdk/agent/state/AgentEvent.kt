package com.autosdk.agent.state

import kotlinx.serialization.json.JsonObject

sealed interface AgentEvent {
    data object BootCompleted : AgentEvent

    data object PackageReplaced : AgentEvent

    data object ServiceConnected : AgentEvent

    data object ServiceInterrupted : AgentEvent

    data object ServiceDestroyed : AgentEvent

    data object ConnectRequested : AgentEvent

    data object BackoffElapsed : AgentEvent

    data object SocketOpened : AgentEvent

    data class SocketOpenFailed(val reason: String) : AgentEvent

    data class HelloAccepted(
        val sessionId: String,
        val atEpochMs: Long,
    ) : AgentEvent

    data class ResumeAccepted(
        val sessionId: String,
        val atEpochMs: Long,
    ) : AgentEvent

    data class ResumeRejected(val reason: String) : AgentEvent

    data class SocketClosed(val reason: String) : AgentEvent

    data class SocketFailed(val reason: String) : AgentEvent

    data object HeartbeatTick : AgentEvent

    data class HeartbeatAccepted(val atEpochMs: Long) : AgentEvent

    data class ObserveStarted(val requestId: String) : AgentEvent

    data class ObserveFinished(val requestId: String) : AgentEvent

    data class ObserveFailed(
        val requestId: String,
        val reason: String,
    ) : AgentEvent

    data class QueryStarted(val requestId: String) : AgentEvent

    data class QueryFinished(val requestId: String) : AgentEvent

    data class QueryFailed(
        val requestId: String,
        val reason: String,
    ) : AgentEvent

    data class ExecuteRequested(val requestId: String) : AgentEvent

    data class SnapshotBeforeFailed(
        val requestId: String,
        val reason: String,
    ) : AgentEvent

    data class SnapshotBeforeBuilt(val requestId: String) : AgentEvent

    data class ActionSucceeded(val requestId: String) : AgentEvent

    data class ActionFailed(
        val requestId: String,
        val reason: String,
    ) : AgentEvent

    data class SettleCompleted(
        val requestId: String,
        val timedOut: Boolean,
    ) : AgentEvent

    data class SnapshotAfterBuilt(val requestId: String) : AgentEvent

    data class ResponseSent(val requestId: String) : AgentEvent

    data class WindowStateChanged(val params: JsonObject) : AgentEvent
}
