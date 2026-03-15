package com.autosdk.agent.observation

import android.graphics.Rect
import android.view.accessibility.AccessibilityNodeInfo
import java.time.Instant
import java.util.UUID

/**
 * Returns true when a node should be included in the snapshot.
 *
 * - [visible] && [hasArea]: normal visible node occupying screen space.
 * - [hasId]: resource ID present — server can target it even if temporarily off-screen.
 * - [hasText]: carries text or content description — useful for text-based selectors.
 *
 * Invisible, zero-area nodes with no ID or text are pure layout containers
 * that carry no information the server can act on. They are excluded to
 * keep the snapshot compact, but their children are still traversed.
 */
internal fun shouldIncludeNode(
    visible: Boolean,
    hasArea: Boolean,
    hasId: Boolean,
    hasText: Boolean,
): Boolean = (visible && hasArea) || hasId || hasText

/**
 * Builds a [UiSnapshot] by traversing one or more live accessibility node trees.
 *
 * Multiple roots are accepted to support multi-window scenarios: when a system dialog
 * or permission prompt overlays the app, it appears as a separate window. Passing all
 * window roots produces a single coherent snapshot that includes the dialog, which is
 * essential for automation that must interact with "Allow / Deny" prompts.
 *
 * ### Filtering
 * Nodes are excluded when they carry no useful information AND are invisible or
 * have no screen area. Pure layout containers that are not visible and have no
 * resourceId, text, or content description are noise for the server and are dropped.
 *
 * ### Node recycling
 * All root nodes passed to [build] are recycled by this function after traversal.
 * Child nodes obtained via [AccessibilityNodeInfo.getChild] are recycled inline.
 * Callers must not recycle roots after passing them here.
 */
object SnapshotBuilder {

    fun build(
        roots: List<AccessibilityNodeInfo>,
        deviceId: String,
        packageName: String?,
        activityName: String?,
        hasSystemWindow: Boolean = false,
    ): UiSnapshot {
        val snapshotId = UUID.randomUUID().toString()
        val targets = mutableListOf<UiTarget>()

        for (root in roots) {
            traverseNode(root, targets, snapshotId)
            root.recycle()
        }

        val screenState = when {
            targets.any { it.uiRole == "loading" && it.enabled } -> "loading"
            hasSystemWindow -> "dialog"
            else -> "ready"
        }
        val semanticProjection =
            buildSemanticProjection(
                rawTargets = targets,
                packageName = packageName,
                activityName = activityName,
                screenState = screenState,
            )
        val focusedTargetId = targets.firstOrNull { it.focused }?.targetId

        return UiSnapshot(
            snapshotId = snapshotId,
            deviceId = deviceId,
            packageName = packageName,
            activityName = activityName,
            screenState = screenState,
            focusedTargetId = focusedTargetId,
            semantic = semanticProjection.semantic,
            capturedAt = Instant.now().toString(),
            targets = semanticProjection.targets,
        )
    }

    private fun traverseNode(
        node: AccessibilityNodeInfo,
        targets: MutableList<UiTarget>,
        snapshotId: String,
    ) {
        val bounds = Rect()
        node.getBoundsInScreen(bounds)

        val hasArea = bounds.width() > 0 && bounds.height() > 0
        val visible = node.isVisibleToUser
        val hasId = !node.viewIdResourceName.isNullOrBlank()
        val hasText = !node.text.isNullOrBlank() || !node.contentDescription.isNullOrBlank()

        if (!shouldIncludeNode(visible, hasArea, hasId, hasText)) {
            // Still recurse — a visible child may live under an invisible container.
            for (i in 0 until node.childCount) {
                val child = node.getChild(i) ?: continue
                traverseNode(child, targets, snapshotId)
                child.recycle()
            }
            return
        }

        val index = targets.size
        val targetId = buildTargetId(node, snapshotId, index)

        val actionable = node.isClickable || node.isLongClickable ||
                node.isCheckable || node.isEditable || node.isFocusable

        val uiRole = classifyUiRole(
            className = node.className?.toString(),
            isEditable = node.isEditable,
            isClickable = node.isClickable,
            isCheckable = node.isCheckable,
            isScrollable = node.isScrollable,
        )
        val label = if (uiRole == "input" || uiRole == "checkbox" || uiRole == "switch" || uiRole == "radio")
            node.contentDescription?.toString()?.takeIf { it.isNotBlank() }
        else null

        targets.add(
            UiTarget(
                targetId = targetId,
                role = node.className?.toString()?.substringAfterLast('.'),
                uiRole = uiRole,
                label = label,
                semanticKey = null,
                formKey = null,
                text = node.text?.toString()?.takeIf { it.isNotBlank() },
                contentDesc = node.contentDescription?.toString()?.takeIf { it.isNotBlank() },
                resourceId = node.viewIdResourceName?.takeIf { it.isNotBlank() },
                packageName = node.packageName?.toString()?.takeIf { it.isNotBlank() },
                bounds = intArrayOf(bounds.left, bounds.top, bounds.right, bounds.bottom),
                actionable = actionable,
                enabled = node.isEnabled,
                checked = if (node.isCheckable) node.isChecked else null,
                selected = node.isSelected,
                scrollable = node.isScrollable,
                focused = node.isFocused,
                password = node.isPassword,
            )
        )

        for (i in 0 until node.childCount) {
            val child = node.getChild(i) ?: continue
            traverseNode(child, targets, snapshotId)
            child.recycle()
        }
    }

    /**
     * Produces a targetId that is stable within a single snapshot traversal.
     *
     * Format: `{snapshotId}_{resourceBasename}_{index}` when a resource id is available,
     * or `{snapshotId}_node_{index}` as fallback.
     *
     * The index suffix lets [ActionExecutor] locate the same node in a re-traversal
     * using [com.autosdk.agent.action.SelectorKind.TARGET_ID].
     */
    private fun buildTargetId(node: AccessibilityNodeInfo, snapshotId: String, index: Int): String {
        val resourceBase = node.viewIdResourceName?.substringAfterLast('/')
        return if (!resourceBase.isNullOrBlank()) {
            "${snapshotId}_${resourceBase}_$index"
        } else {
            "${snapshotId}_node_$index"
        }
    }
}
