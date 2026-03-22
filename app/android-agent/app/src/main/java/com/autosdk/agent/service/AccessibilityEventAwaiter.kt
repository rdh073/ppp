package com.autosdk.agent.service

import android.util.Log
import android.view.accessibility.AccessibilityEvent
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

internal class AccessibilityEventAwaiter(
    private val logTag: String,
) {
    private val watchers = CopyOnWriteArrayList<EventWatcher>()

    fun notify(event: AccessibilityEvent) {
        if (watchers.isEmpty()) return

        val eventPkg = event.packageName?.toString()
        val eventText = event.text?.joinToString(" ")
        for (watcher in watchers) {
            if (event.eventType !in watcher.eventTypes) continue
            if (watcher.pkg != null && watcher.pkg != eventPkg) continue
            if (
                watcher.textContains != null &&
                    (eventText == null || !eventText.contains(watcher.textContains, ignoreCase = true))
            ) {
                continue
            }
            watcher.latch.countDown()
        }
    }

    fun await(
        kind: String,
        pkg: String?,
        textContains: String?,
        timeoutMs: Long,
    ): Boolean {
        val eventTypes: Set<Int> =
            when (kind) {
                "activity_created", "window_state_changed" ->
                    setOf(AccessibilityEvent.TYPE_WINDOW_STATE_CHANGED)
                "content_changed" ->
                    setOf(AccessibilityEvent.TYPE_WINDOW_CONTENT_CHANGED)
                else -> {
                    Log.w(logTag, "awaitAccessibilityEvent: unknown kind '$kind'")
                    return false
                }
            }

        val latch = CountDownLatch(1)
        val watcher = EventWatcher(eventTypes, pkg, textContains, latch)
        watchers.add(watcher)
        return try {
            latch.await(timeoutMs, TimeUnit.MILLISECONDS)
        } finally {
            watchers.remove(watcher)
        }
    }

    private data class EventWatcher(
        val eventTypes: Set<Int>,
        val pkg: String?,
        val textContains: String?,
        val latch: CountDownLatch,
    )
}
