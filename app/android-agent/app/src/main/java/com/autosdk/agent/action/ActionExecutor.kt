package com.autosdk.agent.action

import android.accessibilityservice.AccessibilityService
import android.content.Intent
import android.graphics.Rect
import android.os.Build
import android.os.Bundle
import android.util.Log
import android.view.accessibility.AccessibilityNodeInfo

private const val TAG = "ActionExecutor"

/**
 * Executes [AutomationAction] values against the live accessibility tree.
 *
 * Returns [ActionResult] so callers can surface distinct failure classes
 * to the server rather than collapsing everything to a single error code.
 *
 * All methods are synchronous and should be called from a coroutine context
 * that tolerates blocking (e.g. Dispatchers.IO).
 */
class ActionExecutor(private val service: AccessibilityService) {

    fun execute(action: AutomationAction): ActionResult = when (action) {
        is AutomationAction.Home ->
            global(AccessibilityService.GLOBAL_ACTION_HOME)

        is AutomationAction.Back ->
            global(AccessibilityService.GLOBAL_ACTION_BACK)

        is AutomationAction.Wake ->
            global(AccessibilityService.GLOBAL_ACTION_HOME)

        is AutomationAction.CloseApp ->
            global(AccessibilityService.GLOBAL_ACTION_HOME)

        is AutomationAction.Click -> withNode(action.selector) { node ->
            if (!node.isEnabled) return@withNode ActionResult.Failed(
                "target_not_actionable", "Node is disabled: ${action.selector}"
            )
            if (!node.isClickable) return@withNode ActionResult.Failed(
                "target_not_actionable", "Node is not clickable: ${action.selector}"
            )
            dispatchAction(node, AccessibilityNodeInfo.ACTION_CLICK, action.selector)
        }

        is AutomationAction.LongPress -> withNode(action.selector) { node ->
            if (!node.isEnabled) return@withNode ActionResult.Failed(
                "target_not_actionable", "Node is disabled: ${action.selector}"
            )
            dispatchAction(node, AccessibilityNodeInfo.ACTION_LONG_CLICK, action.selector)
        }

        is AutomationAction.InputText -> withNode(action.selector) { node ->
            if (!node.isEnabled) return@withNode ActionResult.Failed(
                "target_not_actionable", "Node is disabled: ${action.selector}"
            )
            val args = Bundle().apply {
                putCharSequence(
                    AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE,
                    action.value,
                )
            }
            dispatchAction(node, AccessibilityNodeInfo.ACTION_SET_TEXT, action.selector, args)
        }

        is AutomationAction.DeleteText -> withNode(action.selector) { node ->
            if (!node.isEnabled) return@withNode ActionResult.Failed(
                "target_not_actionable", "Node is disabled: ${action.selector}"
            )
            val args = Bundle().apply {
                putCharSequence(
                    AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE,
                    "",
                )
            }
            dispatchAction(node, AccessibilityNodeInfo.ACTION_SET_TEXT, action.selector, args)
        }

        is AutomationAction.Drag -> ActionResult.Failed(
            "capability_unavailable",
            "Drag requires GestureDescription and is not yet implemented",
        )

        is AutomationAction.Scroll -> {
            val scrollAction = if (action.direction == ScrollDirection.FORWARD)
                AccessibilityNodeInfo.ACTION_SCROLL_FORWARD
            else
                AccessibilityNodeInfo.ACTION_SCROLL_BACKWARD

            if (action.selector != null) {
                withNode(action.selector) { node ->
                    if (!node.isScrollable) return@withNode ActionResult.Failed(
                        "target_not_actionable", "Node is not scrollable: ${action.selector}"
                    )
                    dispatchAction(node, scrollAction, action.selector)
                }
            } else {
                val root = service.rootInActiveWindow
                    ?: return ActionResult.Failed("device_unavailable", "No active window")
                val result = root.performAction(scrollAction)
                root.recycle()
                if (result) ActionResult.Ok
                else ActionResult.Failed("input_rejected", "Scroll on root window rejected by OS")
            }
        }

        is AutomationAction.OpenApp -> {
            val intent = service.packageManager
                .getLaunchIntentForPackage(action.packageName)
            if (intent == null) {
                ActionResult.Failed(
                    "target_not_found",
                    "No launch intent for package: ${action.packageName}",
                )
            } else {
                intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                service.startActivity(intent)
                ActionResult.Ok
            }
        }

        is AutomationAction.Screenshot -> {
            if (Build.VERSION.SDK_INT < 30) {
                ActionResult.Failed("capability_unavailable", "Screenshot requires API 30+")
            } else {
                // Captured asynchronously by the caller via AccessibilityService.takeScreenshot().
                ActionResult.Ok
            }
        }
    }

    /**
     * Finds the first accessibility node matching [selector] in the live tree.
     * Caller is responsible for recycling the returned node.
     */
    fun findNode(selector: Selector): AccessibilityNodeInfo? {
        val root = service.rootInActiveWindow ?: return null
        return when (selector.kind) {
            SelectorKind.TEXT ->
                root.findAccessibilityNodeInfosByText(selector.value).firstOrNull()

            SelectorKind.RESOURCE_ID ->
                root.findAccessibilityNodeInfosByViewId(selector.value).firstOrNull()

            SelectorKind.CONTENT_DESC ->
                // findAccessibilityNodeInfosByText searches both text and contentDescription.
                root.findAccessibilityNodeInfosByText(selector.value).firstOrNull()

            SelectorKind.TARGET_ID ->
                findNodeByTargetId(root, selector.value)

            SelectorKind.BOUNDS ->
                findNodeByBounds(root, selector.value)

            SelectorKind.PACKAGE_NAME ->
                findNodeByPackageName(root, selector.value)
        }
    }

    // ---- private helpers ----

    private fun global(action: Int): ActionResult {
        val ok = service.performGlobalAction(action)
        return if (ok) ActionResult.Ok
        else ActionResult.Failed("input_rejected", "Global action $action rejected by OS")
    }

    private inline fun withNode(
        selector: Selector,
        block: (AccessibilityNodeInfo) -> ActionResult,
    ): ActionResult {
        val node = findNode(selector)
            ?: return ActionResult.Failed("target_not_found", "No node for selector: $selector")
        return try {
            block(node)
        } finally {
            node.recycle()
        }
    }

    private fun dispatchAction(
        node: AccessibilityNodeInfo,
        action: Int,
        selector: Selector,
        args: Bundle? = null,
    ): ActionResult {
        val ok = if (args != null) node.performAction(action, args) else node.performAction(action)
        return if (ok) ActionResult.Ok
        else ActionResult.Failed("input_rejected", "Action $action rejected by OS for: $selector")
    }

    /**
     * Locates the node at the tree-order index encoded in [targetId].
     * Format produced by [SnapshotBuilder]: `{snapshotId}_{name}_{index}`.
     *
     * All nodes that are not the target are recycled to avoid leaking
     * system-held AccessibilityNodeInfo objects.
     */
    private fun findNodeByTargetId(root: AccessibilityNodeInfo, targetId: String): AccessibilityNodeInfo? {
        val index = targetId.substringAfterLast('_').toIntOrNull() ?: return null
        val nodes = mutableListOf<AccessibilityNodeInfo>()
        collectAllNodes(root, nodes)
        val target = nodes.getOrNull(index)
        nodes.forEachIndexed { i, node -> if (i != index) node.recycle() }
        return target
    }

    /** Parses `"left,top,right,bottom"` and finds the node with matching screen bounds. */
    private fun findNodeByBounds(root: AccessibilityNodeInfo, boundsStr: String): AccessibilityNodeInfo? {
        val parts = boundsStr.split(",").map { it.trim().toIntOrNull() }
        if (parts.size != 4 || parts.any { it == null }) return null
        val target = Rect(parts[0]!!, parts[1]!!, parts[2]!!, parts[3]!!)
        return findNodeByBoundsRecursive(root, target)
    }

    private fun findNodeByBoundsRecursive(node: AccessibilityNodeInfo, target: Rect): AccessibilityNodeInfo? {
        val bounds = Rect()
        node.getBoundsInScreen(bounds)
        if (bounds == target) return node
        for (i in 0 until node.childCount) {
            val child = node.getChild(i) ?: continue
            val result = findNodeByBoundsRecursive(child, target)
            if (result != null) {
                if (result !== child) child.recycle()
                return result
            }
            child.recycle()
        }
        return null
    }

    private fun findNodeByPackageName(root: AccessibilityNodeInfo, packageName: String): AccessibilityNodeInfo? {
        if (root.packageName?.toString() == packageName) return root
        for (i in 0 until root.childCount) {
            val child = root.getChild(i) ?: continue
            val result = findNodeByPackageName(child, packageName)
            if (result != null) {
                if (result !== child) child.recycle()
                return result
            }
            child.recycle()
        }
        return null
    }

    private fun collectAllNodes(node: AccessibilityNodeInfo, out: MutableList<AccessibilityNodeInfo>) {
        out.add(node)
        for (i in 0 until node.childCount) {
            val child = node.getChild(i) ?: continue
            collectAllNodes(child, out)
        }
    }
}
