package com.autosdk.agent.state

import android.content.Context
import android.content.SharedPreferences
import java.util.UUID

private const val PREFS_NAME = "agent_state"
private const val KEY_AGENT_INSTANCE_ID = "agent_instance_id"
private const val KEY_SESSION_ID = "session_id"
private const val KEY_LAST_REGISTRATION_AT = "last_registration_at_epoch_ms"
private const val KEY_INFLIGHT_REQUEST_ID = "inflight_request_id"
private const val KEY_LAST_OUTBOUND_EVENT_SEQ_NO = "last_outbound_event_seq_no"
private const val KEY_PENDING_ACCESSIBILITY_DISABLED_SEQ_NO = "pending_accessibility_disabled_seq_no"
private const val KEY_PENDING_ACCESSIBILITY_DISABLED_REASON = "pending_accessibility_disabled_reason"

data class PendingAccessibilityDisabledEvent(
    val seqNo: Long,
    val reason: String,
)

data class PersistedAgentState(
    val agentInstanceId: String,
    val sessionId: String?,
    val lastRegistrationAtEpochMs: Long?,
    val inflightRequestId: String?,
    val lastOutboundEventSeqNo: Long = 0L,
    val pendingAccessibilityDisabledEvent: PendingAccessibilityDisabledEvent? = null,
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

    fun persistLastOutboundEventSeqNo(seqNo: Long)

    fun persistPendingAccessibilityDisabledEvent(event: PendingAccessibilityDisabledEvent)

    fun clearPendingAccessibilityDisabledEvent()
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
            lastOutboundEventSeqNo = prefs.getLong(KEY_LAST_OUTBOUND_EVENT_SEQ_NO, 0L),
            pendingAccessibilityDisabledEvent = readPendingAccessibilityDisabledEvent(),
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

    override fun persistLastOutboundEventSeqNo(seqNo: Long) {
        prefs.edit().putLong(KEY_LAST_OUTBOUND_EVENT_SEQ_NO, seqNo).apply()
    }

    override fun persistPendingAccessibilityDisabledEvent(event: PendingAccessibilityDisabledEvent) {
        prefs.edit().apply {
            putLong(KEY_PENDING_ACCESSIBILITY_DISABLED_SEQ_NO, event.seqNo)
            putString(KEY_PENDING_ACCESSIBILITY_DISABLED_REASON, event.reason)
        }.apply()
    }

    override fun clearPendingAccessibilityDisabledEvent() {
        prefs.edit().apply {
            remove(KEY_PENDING_ACCESSIBILITY_DISABLED_SEQ_NO)
            remove(KEY_PENDING_ACCESSIBILITY_DISABLED_REASON)
        }.apply()
    }

    private fun getOrCreateAgentInstanceId(): String =
        prefs.getString(KEY_AGENT_INSTANCE_ID, null) ?: UUID.randomUUID().toString().also { id ->
            prefs.edit().putString(KEY_AGENT_INSTANCE_ID, id).apply()
        }

    private fun readPendingAccessibilityDisabledEvent(): PendingAccessibilityDisabledEvent? {
        if (!prefs.contains(KEY_PENDING_ACCESSIBILITY_DISABLED_SEQ_NO)) {
            return null
        }
        val reason = prefs.getString(KEY_PENDING_ACCESSIBILITY_DISABLED_REASON, null)?.trim().orEmpty()
        if (reason.isEmpty()) {
            return null
        }
        return PendingAccessibilityDisabledEvent(
            seqNo = prefs.getLong(KEY_PENDING_ACCESSIBILITY_DISABLED_SEQ_NO, 0L),
            reason = reason,
        )
    }

    companion object {
        fun from(context: Context): SharedPreferencesAgentStateStore =
            SharedPreferencesAgentStateStore(
                context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE),
            )
    }
}
