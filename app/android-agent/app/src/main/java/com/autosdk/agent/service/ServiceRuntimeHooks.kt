package com.autosdk.agent.service

import android.content.Context
import android.util.Log
import com.autosdk.agent.state.AgentRuntimeHooks
import com.autosdk.agent.state.AgentStatus
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

internal class ServiceRuntimeHooks(
    private val context: Context,
    private val scope: CoroutineScope,
    private val notifier: AccessibilityDisabledNotifier,
    private val tag: String,
) : AgentRuntimeHooks {

    private var wasConnected = false

    override fun clearInflightCommand() {
        // Runtime execution event wiring will own this more precisely in PR6.
    }

    override fun onStatusChanged(status: AgentStatus) {
        val isConnected = status.transport == "connected"
        if (isConnected && !wasConnected) {
            scope.launch { notifier.flushPending() }
        }
        wasConnected = isConnected
        AgentNotificationManager.update(context, status)
        Log.d(tag, "status ${status.toDebugString()}")
    }
}
