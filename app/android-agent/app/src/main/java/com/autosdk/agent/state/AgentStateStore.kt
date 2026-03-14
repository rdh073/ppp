package com.autosdk.agent.state

import android.content.Context
import android.content.SharedPreferences
import java.util.UUID

private const val PREFS_NAME = "agent_state"
private const val KEY_AGENT_INSTANCE_ID = "agent_instance_id"
private const val KEY_SESSION_ID = "session_id"
private const val KEY_LAST_REGISTRATION_AT = "last_registration_at_epoch_ms"
private const val KEY_INFLIGHT_REQUEST_ID = "inflight_request_id"

data class PersistedAgentState(
    val agentInstanceId: String,
    val sessionId: String?,
    val lastRegistrationAtEpochMs: Long?,
    val inflightRequestId: String?,
)

interface AgentStateStore {
    fun read(): PersistedAgentState

    fun persistSession(
        sessionId: String,
        lastRegistrationAtEpochMs: Long?,
    )

    fun clearSession()

    fun persistInflightRequestId(requestId: String)

    fun clearInflightRequestId()
}

class SharedPreferencesAgentStateStore private constructor(
    private val prefs: SharedPreferences,
) : AgentStateStore {
    override fun read(): PersistedAgentState =
        PersistedAgentState(
            agentInstanceId = getOrCreateAgentInstanceId(),
            sessionId = prefs.getString(KEY_SESSION_ID, null),
            lastRegistrationAtEpochMs =
                if (prefs.contains(KEY_LAST_REGISTRATION_AT)) {
                    prefs.getLong(KEY_LAST_REGISTRATION_AT, 0L)
                } else {
                    null
                },
            inflightRequestId = prefs.getString(KEY_INFLIGHT_REQUEST_ID, null),
        )

    override fun persistSession(
        sessionId: String,
        lastRegistrationAtEpochMs: Long?,
    ) {
        prefs.edit().apply {
            putString(KEY_SESSION_ID, sessionId)
            if (lastRegistrationAtEpochMs != null) {
                putLong(KEY_LAST_REGISTRATION_AT, lastRegistrationAtEpochMs)
            } else {
                remove(KEY_LAST_REGISTRATION_AT)
            }
        }.apply()
    }

    override fun clearSession() {
        prefs.edit()
            .remove(KEY_SESSION_ID)
            .remove(KEY_LAST_REGISTRATION_AT)
            .apply()
    }

    override fun persistInflightRequestId(requestId: String) {
        prefs.edit().putString(KEY_INFLIGHT_REQUEST_ID, requestId).apply()
    }

    override fun clearInflightRequestId() {
        prefs.edit().remove(KEY_INFLIGHT_REQUEST_ID).apply()
    }

    private fun getOrCreateAgentInstanceId(): String =
        prefs.getString(KEY_AGENT_INSTANCE_ID, null) ?: UUID.randomUUID().toString().also { id ->
            prefs.edit().putString(KEY_AGENT_INSTANCE_ID, id).apply()
        }

    companion object {
        fun from(context: Context): SharedPreferencesAgentStateStore =
            SharedPreferencesAgentStateStore(
                context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE),
            )
    }
}
