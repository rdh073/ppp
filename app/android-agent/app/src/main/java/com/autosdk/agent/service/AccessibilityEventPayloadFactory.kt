package com.autosdk.agent.service

import android.view.accessibility.AccessibilityEvent
import com.autosdk.agent.observation.UiSnapshot
import kotlinx.serialization.json.add
import kotlinx.serialization.json.addJsonObject
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

internal object AccessibilityEventPayloadFactory {
    fun buildScreenChangedParams(
        seqNo: Long,
        eventType: String,
        snapshot: UiSnapshot,
    ) = buildJsonObject {
        put("seqNo", seqNo)
        snapshot.packageName?.let { put("packageName", it) }
        snapshot.activityName?.let { put("className", it) }
        put("eventType", eventType)
        snapshot.screenState?.let { put("screenState", it) }
        snapshot.focusedTargetId?.let { put("focusedTargetId", it) }
        put("text", buildJsonArray {
            snapshot.targets.forEach { target ->
                target.text?.takeIf { it.isNotBlank() }?.let { add(it) }
                target.label?.takeIf { it.isNotBlank() }?.let { add(it) }
            }
        })
        put(
            "ui",
            buildJsonObject {
                put("activeUiKey", snapshot.semantic.activeUiKey)
                put("baseScreenKey", snapshot.semantic.baseScreenKey)
                snapshot.semantic.overlayKey?.let { put("overlayKey", it) }
                put("uiReady", snapshot.semantic.uiReady)
                put("semanticDigest", snapshot.semantic.semanticDigest)
                snapshot.semantic.focusedTargetKey?.let { put("focusedTargetKey", it) }
                put(
                    "forms",
                    buildJsonArray {
                        snapshot.semantic.forms.forEach { form ->
                            addJsonObject {
                                put("formKey", form.formKey)
                                put("fieldKeys", buildJsonArray {
                                    form.fieldKeys.forEach { add(it) }
                                })
                                form.focusedFieldKey?.let { put("focusedFieldKey", it) }
                                put("ready", form.ready)
                            }
                        }
                    },
                )
                put(
                    "buttons",
                    buildJsonArray {
                        snapshot.semantic.buttons.forEach { button ->
                            addJsonObject {
                                put("buttonKey", button.buttonKey)
                                put("enabled", button.enabled)
                                put("visible", button.visible)
                                put("primary", button.primary)
                            }
                        }
                    },
                )
            },
        )
        put("targets", buildJsonArray {
            snapshot.targets.forEach { target ->
                addJsonObject {
                    put("targetId", target.targetId)
                    put("uiRole", target.uiRole)
                    target.label?.let { put("label", it) }
                    target.semanticKey?.let { put("semanticKey", it) }
                    target.formKey?.let { put("formKey", it) }
                    target.text?.let { put("text", it) }
                    target.resourceId?.let { put("resourceId", it) }
                    put("enabled", target.enabled)
                    put("actionable", target.actionable)
                    target.checked?.let { put("checked", it) }
                    put("focused", target.focused)
                }
            }
        })
    }

    fun buildActivityCreatedParams(
        seqNo: Long,
        packageName: String?,
        className: String?,
    ) = buildJsonObject {
        put("seqNo", seqNo)
        packageName?.let { put("packageName", it) }
        className?.let { put("className", it) }
    }

    fun buildNotificationParams(
        seqNo: Long,
        event: AccessibilityEvent,
    ) = buildJsonObject {
        put("seqNo", seqNo)
        event.packageName?.toString()?.let { put("packageName", it) }
        put("text", buildJsonArray {
            event.text?.forEach { t -> t?.toString()?.takeIf { it.isNotBlank() }?.let { add(it) } }
            event.contentDescription?.toString()?.takeIf { it.isNotBlank() }?.let { add(it) }
        })
    }

    fun shouldScheduleSemanticPublish(eventType: Int): Boolean =
        eventType == AccessibilityEvent.TYPE_WINDOW_STATE_CHANGED ||
            eventType == AccessibilityEvent.TYPE_WINDOW_CONTENT_CHANGED ||
            eventType == AccessibilityEvent.TYPE_WINDOWS_CHANGED ||
            eventType == AccessibilityEvent.TYPE_VIEW_TEXT_CHANGED ||
            eventType == AccessibilityEvent.TYPE_VIEW_SCROLLED

    fun eventTypeName(eventType: Int): String =
        when (eventType) {
            AccessibilityEvent.TYPE_WINDOW_STATE_CHANGED -> "TYPE_WINDOW_STATE_CHANGED"
            AccessibilityEvent.TYPE_WINDOW_CONTENT_CHANGED -> "TYPE_WINDOW_CONTENT_CHANGED"
            AccessibilityEvent.TYPE_WINDOWS_CHANGED -> "TYPE_WINDOWS_CHANGED"
            AccessibilityEvent.TYPE_VIEW_TEXT_CHANGED -> "TYPE_VIEW_TEXT_CHANGED"
            AccessibilityEvent.TYPE_VIEW_SCROLLED -> "TYPE_VIEW_SCROLLED"
            else -> "TYPE_UNKNOWN"
        }
}
