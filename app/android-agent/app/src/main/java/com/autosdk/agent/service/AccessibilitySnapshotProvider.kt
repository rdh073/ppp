package com.autosdk.agent.service

import android.accessibilityservice.AccessibilityService
import android.view.accessibility.AccessibilityWindowInfo
import com.autosdk.agent.observation.SnapshotBuilder
import com.autosdk.agent.observation.UiSnapshot

internal class AccessibilitySnapshotProvider(
    private val service: AccessibilityService,
    private val currentActivityNameProvider: () -> String?,
) {
    fun build(deviceId: String): UiSnapshot? {
        val allWindows = service.windows
        val filteredWindows =
            allWindows
                ?.filter { win ->
                    win.type == AccessibilityWindowInfo.TYPE_APPLICATION ||
                        win.type == AccessibilityWindowInfo.TYPE_SYSTEM
                }

        val windowRoots =
            filteredWindows
                ?.mapNotNull { it.root }
                ?.takeIf { it.isNotEmpty() }
                ?: listOfNotNull(service.rootInActiveWindow)

        if (windowRoots.isEmpty()) {
            return null
        }

        val foregroundPkg = windowRoots.firstOrNull()?.packageName?.toString()
        val hasSystemWindow = filteredWindows?.any { it.type == AccessibilityWindowInfo.TYPE_SYSTEM } == true

        return SnapshotBuilder.build(
            roots = windowRoots,
            deviceId = deviceId,
            packageName = foregroundPkg,
            activityName = currentActivityNameProvider(),
            hasSystemWindow = hasSystemWindow,
        )
    }
}
