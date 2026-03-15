package com.autosdk.agent.state

import kotlinx.serialization.json.JsonObject

sealed interface AgentEffect {
    data object ConnectSocket : AgentEffect

    data object DisconnectSocket : AgentEffect

    data object SendHello : AgentEffect

    data class SendResume(val sessionId: String) : AgentEffect

    data class SendDisconnect(val sessionId: String) : AgentEffect

    data object SendHeartbeat : AgentEffect

    data object StartHeartbeatTimer : AgentEffect

    data object StopHeartbeatTimer : AgentEffect

    data class ScheduleBackoff(val attempt: Int) : AgentEffect

    data class PersistSessionId(val sessionId: String) : AgentEffect

    data object ClearSessionId : AgentEffect

    data object ClearInflightCommand : AgentEffect

    data class Log(val message: String) : AgentEffect

    data class PublishUiEvent(val method: String, val params: JsonObject) : AgentEffect
}
