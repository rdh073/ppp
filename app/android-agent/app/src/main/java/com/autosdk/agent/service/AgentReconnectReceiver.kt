package com.autosdk.agent.service

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/**
 * Receives the "Reconnect" tap from the persistent status notification and
 * delegates to [AgentAccessibilityService.requestReconnect].
 *
 * Registered in AndroidManifest with the [ACTION_RECONNECT] intent filter.
 * Not exported — only reachable via [PendingIntent] from this app.
 */
class AgentReconnectReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action == ACTION_RECONNECT) {
            AgentAccessibilityService.instance?.requestReconnect()
        }
    }
}
