package com.autosdk.agent.state

data class AgentStatus(
    val service: String,
    val transport: String,
    val execution: String,
    val registrationMode: String?,
    val reconnectAttempt: Int,
    val sessionId: String?,
    val lastHeartbeatAtEpochMs: Long?,
    val lastRegistrationAtEpochMs: Long?,
    val currentRequestId: String?,
    val lastError: String?,
) {
    companion object {
        fun from(state: AgentState): AgentStatus =
            AgentStatus(
                service = state.service.wireName(),
                transport = state.transport.wireName(),
                execution = state.execution.wireName(),
                registrationMode = state.registrationMode?.wireName(),
                reconnectAttempt = state.reconnectAttempt,
                sessionId = state.sessionId,
                lastHeartbeatAtEpochMs = state.lastHeartbeatAtEpochMs,
                lastRegistrationAtEpochMs = state.lastRegistrationAtEpochMs,
                currentRequestId = state.currentRequestId,
                lastError = state.lastError,
            )
    }

    fun toDebugString(): String =
        buildString {
            append("service=").append(service)
            append(" transport=").append(transport)
            append(" execution=").append(execution)
            registrationMode?.let { append(" registration=").append(it) }
            append(" reconnectAttempt=").append(reconnectAttempt)
            sessionId?.let { append(" sessionId=").append(it) }
            lastHeartbeatAtEpochMs?.let { append(" lastHeartbeatAt=").append(it) }
            lastRegistrationAtEpochMs?.let { append(" lastRegistrationAt=").append(it) }
            currentRequestId?.let { append(" currentRequestId=").append(it) }
            lastError?.let { append(" lastError=").append(it) }
        }
}

fun AgentState.toStatus(): AgentStatus = AgentStatus.from(this)

private fun Enum<*>.wireName(): String = name.lowercase()
