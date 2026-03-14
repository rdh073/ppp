package com.autosdk.agent.boot

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log
import com.autosdk.agent.state.SharedPreferencesAgentStateStore

private const val TAG = "BootCompletedReceiver"

/**
 * Receives BOOT_COMPLETED and MY_PACKAGE_REPLACED intents.
 *
 * The Android accessibility framework auto-restarts [AgentAccessibilityService]
 * after boot if it was previously enabled, so no explicit restart logic is
 * needed here. This receiver clears any stale in-flight command marker so the
 * next reconnect starts from a fresh observation boundary.
 *
 * ADR invariant: reconnect must start from a fresh observation boundary, not
 * from an assumed partially-completed UI state.
 */
class BootCompletedReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        SharedPreferencesAgentStateStore.from(context).clearInflightRequestId()

        when (intent.action) {
            Intent.ACTION_BOOT_COMPLETED ->
                Log.i(TAG, "Device booted — cleared in-flight marker before auto-connect")
            Intent.ACTION_MY_PACKAGE_REPLACED ->
                Log.i(TAG, "Agent package replaced — cleared in-flight marker before reconnect")
        }
    }
}
