package com.autosdk.agent.service

import android.util.Log
import com.autosdk.agent.state.AgentStateStore
import com.autosdk.agent.state.AgentTransportDriver
import com.autosdk.agent.state.PendingAccessibilityDisabledEvent
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

private const val METHOD_ANDROID_ACCESSIBILITY_DISABLED = "android.accessibility.disabled"

internal class AccessibilityDisabledNotifier(
    private val logTag: String,
    private val stateStoreProvider: () -> AgentStateStore?,
    private val transportDriverProvider: () -> AgentTransportDriver?,
    private val nextSeqNo: () -> Long,
) {
    suspend fun notify(reason: String) {
        val seqNo = nextSeqNo()
        val pendingEvent = PendingAccessibilityDisabledEvent(seqNo = seqNo, reason = reason)
        stateStoreProvider()?.persistLastOutboundEventSeqNo(seqNo)
        stateStoreProvider()?.persistPendingAccessibilityDisabledEvent(pendingEvent)
        flushPending()
    }

    suspend fun flushPending() {
        val store = stateStoreProvider() ?: return
        val pendingEvent = store.read().pendingAccessibilityDisabledEvent ?: return
        val payload =
            buildJsonObject {
                put("seqNo", pendingEvent.seqNo)
                put("reason", pendingEvent.reason)
            }

        val sent =
            runCatching {
                transportDriverProvider()?.sendNotification(
                    method = METHOD_ANDROID_ACCESSIBILITY_DISABLED,
                    params = payload,
                ) == true
            }.getOrElse { error ->
                Log.w(logTag, "Failed to notify accessibility disabled: ${error.message}")
                false
            }

        if (sent) {
            store.clearPendingAccessibilityDisabledEvent()
        }
    }
}
