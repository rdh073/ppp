package com.autosdk.agent.state

enum class AgentServicePhase {
    AWAITING_SERVICE,
    ACTIVE,
    INTERRUPTED,
    STOPPED,
}

enum class AgentTransportPhase {
    DISCONNECTED,
    CONNECTING,
    REGISTERING,
    CONNECTED,
    BACKOFF_WAIT,
}

enum class AgentRegistrationMode {
    HELLO,
    RESUME,
}

enum class AgentExecutionPhase {
    IDLE,
    HANDLING_OBSERVE,
    HANDLING_QUERY,
    OBSERVING_BEFORE_ACTION,
    EXECUTING_ACTION,
    SETTLING,
    OBSERVING_AFTER_ACTION,
    RESPONDING,
}

data class AgentState(
    val service: AgentServicePhase = AgentServicePhase.AWAITING_SERVICE,
    val transport: AgentTransportPhase = AgentTransportPhase.DISCONNECTED,
    val execution: AgentExecutionPhase = AgentExecutionPhase.IDLE,
    val registrationMode: AgentRegistrationMode? = null,
    val reconnectAttempt: Int = 0,
    val sessionId: String? = null,
    val lastHeartbeatAtEpochMs: Long? = null,
    val lastRegistrationAtEpochMs: Long? = null,
    val currentRequestId: String? = null,
    val lastPublishedUiDigest: String? = null,
    val lastError: String? = null,
) {
    companion object {
        fun initial(): AgentState = AgentState()
    }
}
