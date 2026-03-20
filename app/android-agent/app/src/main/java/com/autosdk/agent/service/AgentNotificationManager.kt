package com.autosdk.agent.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.graphics.drawable.Icon
import android.provider.Settings
import com.autosdk.agent.state.AgentStatus

internal const val NOTIFICATION_ID = 1001
internal const val CHANNEL_ID = "agent_status"
internal const val ACTION_RECONNECT = "com.autosdk.agent.ACTION_RECONNECT"

/**
 * Manages the persistent status notification shown while the accessibility
 * service is active.
 *
 * Notification content:
 *   Title  : "ppp Agent"
 *   Body   : human-readable transport status + session hint
 *   Actions: "Reconnect" (only when disconnected/backoff) · "Accessibility Settings"
 *
 * Call [createChannel] once before the first [update].
 * Call [update] from [AgentRuntimeHooks.onStatusChanged].
 * Call [cancel] from [AgentAccessibilityService.onDestroy].
 */
internal object AgentNotificationManager {

    fun createChannel(context: Context) {
        val channel = NotificationChannel(
            CHANNEL_ID,
            "Agent Status",
            NotificationManager.IMPORTANCE_LOW,
        ).apply {
            description = "ppp agent connection status"
            setShowBadge(false)
        }
        notificationManager(context).createNotificationChannel(channel)
    }

    fun build(context: Context, status: AgentStatus): Notification {
        val builder = Notification.Builder(context, CHANNEL_ID)
            .setSmallIcon(android.R.drawable.ic_dialog_info)
            .setContentTitle("ppp Agent")
            .setContentText(statusLine(status))
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setShowWhen(false)

        if (isReconnectable(status)) {
            builder.addAction(
                Notification.Action.Builder(
                    Icon.createWithResource(context, android.R.drawable.ic_menu_rotate),
                    "Reconnect",
                    reconnectIntent(context),
                ).build(),
            )
        }

        builder.addAction(
            Notification.Action.Builder(
                Icon.createWithResource(context, android.R.drawable.ic_menu_preferences),
                "Accessibility Settings",
                accessibilitySettingsIntent(context),
            ).build(),
        )

        return builder.build()
    }

    fun update(context: Context, status: AgentStatus) {
        notificationManager(context).notify(NOTIFICATION_ID, build(context, status))
    }

    fun cancel(context: Context) {
        notificationManager(context).cancel(NOTIFICATION_ID)
    }

    // --- helpers ---

    private fun statusLine(status: AgentStatus): String = when (status.transport) {
        "connected" -> {
            val hint = status.sessionId?.take(8)?.let { " · $it…" } ?: ""
            "Connected$hint"
        }
        "connecting" -> "Connecting…"
        "registering" -> "Registering…"
        "backoff_wait" -> "Reconnecting… (attempt ${status.reconnectAttempt})"
        else -> when (status.service) {
            "interrupted" -> "Service interrupted"
            "stopped"     -> "Service stopped"
            else          -> "Disconnected"
        }
    }

    /** Show "Reconnect" only when the transport is idle and can accept a connect request. */
    private fun isReconnectable(status: AgentStatus): Boolean =
        status.transport == "disconnected" || status.transport == "backoff_wait"

    private fun reconnectIntent(context: Context): PendingIntent =
        PendingIntent.getBroadcast(
            context,
            0,
            Intent(ACTION_RECONNECT).setPackage(context.packageName),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )

    private fun accessibilitySettingsIntent(context: Context): PendingIntent =
        PendingIntent.getActivity(
            context,
            0,
            Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )

    private fun notificationManager(context: Context): NotificationManager =
        context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
}
