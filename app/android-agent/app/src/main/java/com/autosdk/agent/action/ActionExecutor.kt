package com.autosdk.agent.action

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.GestureDescription
import android.content.Intent
import android.graphics.Path
import android.graphics.Rect
import android.os.Build
import android.os.Bundle
import android.util.Log
import android.view.accessibility.AccessibilityNodeInfo
import android.view.accessibility.AccessibilityWindowInfo
import com.autosdk.agent.observation.SnapshotBuilder
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

private const val TAG = "ActionExecutor"

/**
 * Executes [AutomationAction] values against the live accessibility tree.
 *
 * Returns [ActionResult] so callers can surface distinct failure classes
 * to the server rather than collapsing everything to a single error code.
 *
 * All methods are synchronous and should be called from a coroutine context
 * that tolerates blocking (e.g. Dispatchers.IO).
 *
 * ### Node recycling ownership
 * - Nodes returned from [withNode] are recycled by [withNode]'s `try/finally` block.
 *   The [block] lambda must not recycle the node it receives.
 * - Nodes returned from [findSelfOrAncestor] are owned by the caller; the Click and
 *   LongPress handlers recycle them in their own `finally` blocks when the found
 *   node differs from the starting node (i.e. an ancestor was climbed to).
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

        is AutomationAction.Click -> {
            if (action.selector.kind == SelectorKind.COORDINATE) {
                dispatchCoordinateTap(action.selector.value)
            } else {
                withNode(action.selector) { node ->
                    val clickNode = findSelfOrAncestor(node) { it.isClickable }
                        ?: return@withNode ActionResult.Failed(
                            "target_not_actionable",
                            "Node and ancestors are not clickable: ${action.selector}",
                        )
                    try {
                        if (!clickNode.isEnabled) return@withNode ActionResult.Failed(
                            "target_not_actionable", "Node is disabled: ${action.selector}"
                        )
                        dispatchAction(clickNode, AccessibilityNodeInfo.ACTION_CLICK, action.selector)
                    } finally {
                        if (clickNode !== node) clickNode.recycle()
                    }
                }
            }
        }

        is AutomationAction.LongPress -> withNode(action.selector) { node ->
            val longClickNode = findSelfOrAncestor(node) { it.isLongClickable }
                ?: return@withNode ActionResult.Failed(
                    "target_not_actionable",
                    "Node and ancestors are not long-clickable: ${action.selector}",
                )
            try {
                if (!longClickNode.isEnabled) return@withNode ActionResult.Failed(
                    "target_not_actionable", "Node is disabled: ${action.selector}"
                )
                dispatchAction(longClickNode, AccessibilityNodeInfo.ACTION_LONG_CLICK, action.selector)
            } finally {
                if (longClickNode !== node) longClickNode.recycle()
            }
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

        is AutomationAction.Swipe -> dispatchSwipe(action)

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

        is AutomationAction.OpenIntent -> {
            try {
                val intent = Intent(action.intentAction).apply {
                    addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                    action.packageName?.takeIf { it.isNotBlank() }?.let { setPackage(it) }
                }
                service.startActivity(intent)
                ActionResult.Ok
            } catch (t: Throwable) {
                ActionResult.Failed(
                    "input_rejected",
                    "Failed to start intent ${action.intentAction}: ${t.message ?: t::class.java.simpleName}",
                )
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

        is AutomationAction.FillForm -> run {
            for (field in action.fields) {
                val result = withNode(field.selector) { node ->
                    if (!node.isEnabled) return@withNode ActionResult.Failed(
                        "target_not_actionable", "Node is disabled: ${field.selector}"
                    )
                    val args = Bundle().apply {
                        putCharSequence(
                            AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE,
                            field.value,
                        )
                    }
                    dispatchAction(node, AccessibilityNodeInfo.ACTION_SET_TEXT, field.selector, args)
                }
                if (result is ActionResult.Failed) return@run result
            }
            ActionResult.Ok
        }
    }

    /**
     * Finds the first accessibility node matching [selector] in the live tree.
     * Caller is responsible for recycling the returned node.
     *
     * For TEXT / RESOURCE_ID / CONTENT_DESC / SEMANTIC_KEY selectors the search spans ALL
     * accessibility windows so that nodes in non-focused floating windows
     * (e.g. SubSettings in Waydroid freeform mode) are included.
     */
    fun findNode(selector: Selector): AccessibilityNodeInfo? {
        return when (selector.kind) {
            SelectorKind.TEXT, SelectorKind.RESOURCE_ID, SelectorKind.CONTENT_DESC, SelectorKind.SEMANTIC_KEY ->
                findInAllWindows(selector)

            else -> {
                val root = service.rootInActiveWindow ?: return null
                when (selector.kind) {
                    SelectorKind.TARGET_ID ->
                        findNodeByTargetId(root, selector.value)
                    SelectorKind.BOUNDS ->
                        findNodeByBounds(root, selector.value)
                    SelectorKind.PACKAGE_NAME ->
                        findNodeByPackageName(root, selector.value)
                    else -> null
                }
            }
        }
    }

    /**
     * Searches all accessibility windows (not just the focused one) for
     * TEXT / RESOURCE_ID / CONTENT_DESC / SEMANTIC_KEY selectors.
     */
    private fun findInAllWindows(selector: Selector): AccessibilityNodeInfo? {
        val windows = service.windows?.takeIf { it.isNotEmpty() }
            ?: return service.rootInActiveWindow?.let { root ->
                val node = searchAcrossSnapshot(root, selector)
                if (node == null) {
                    root.recycle()
                }
                node
            }
        for (window in windows) {
            val root = window.root ?: continue
            val node = searchAcrossSnapshot(root, selector)
            if (node != null) {
                return node
            }
            root.recycle()
        }
        return null
    }

    private fun searchAcrossSnapshot(root: AccessibilityNodeInfo, selector: Selector): AccessibilityNodeInfo? =
        when (selector.kind) {
            SelectorKind.TEXT ->
                findNodeByText(root, selector.value)
            SelectorKind.CONTENT_DESC ->
                findNodeByContentDescription(root, selector.value)
            SelectorKind.RESOURCE_ID ->
                findNodeByResourceId(root, selector.value)
            SelectorKind.SEMANTIC_KEY ->
                findNodeBySemanticKey(root, selector.value)
            else -> null
        }

    private fun findNodeBySemanticKey(
        root: AccessibilityNodeInfo,
        semanticKey: String,
    ): AccessibilityNodeInfo? {
        val windowRoots =
            service.windows
                ?.filter { win ->
                    win.type == AccessibilityWindowInfo.TYPE_APPLICATION ||
                        win.type == AccessibilityWindowInfo.TYPE_SYSTEM
                }
                ?.mapNotNull { it.root }
                ?.takeIf { it.isNotEmpty() }
                ?: listOf(root)
        val snapshotRoots = windowRoots.map { AccessibilityNodeInfo.obtain(it) }

        val foregroundPkg = windowRoots.firstOrNull()?.packageName?.toString()
        val hasSystemWindow = service.windows?.any { it.type == AccessibilityWindowInfo.TYPE_SYSTEM } == true
        val snapshot =
            SnapshotBuilder.build(
                roots = snapshotRoots,
                deviceId = "",
                packageName = foregroundPkg,
                activityName = null,
                hasSystemWindow = hasSystemWindow,
            )

        val target = snapshot.targets.firstOrNull { it.semanticKey == semanticKey } ?: return null

        return when {
            !target.resourceId.isNullOrBlank() ->
                searchAcrossSnapshot(root, Selector(SelectorKind.RESOURCE_ID, target.resourceId))

            !target.label.isNullOrBlank() ->
                searchAcrossSnapshot(root, Selector(SelectorKind.CONTENT_DESC, target.label))

            !target.text.isNullOrBlank() ->
                searchAcrossSnapshot(root, Selector(SelectorKind.TEXT, target.text))

            else ->
                findNodeByTargetId(root, target.targetId)
        }
    }

    // ---- private helpers ----

    /**
     * Issues a single-point tap gesture at absolute screen coordinates.
     * Blank [value] is treated as a no-op (returns [ActionResult.Ok] immediately).
     * Requires API 24+ (minSdk = 26, so always satisfied).
     */
    private fun dispatchCoordinateTap(value: String): ActionResult {
        if (value.isBlank()) return ActionResult.Ok

        val parts = value.split(",")
        if (parts.size != 2) {
            return ActionResult.Failed(
                "input_rejected",
                "Invalid coordinate format, expected 'x,y': $value",
            )
        }
        val x = parts[0].trim().toIntOrNull()
            ?: return ActionResult.Failed("input_rejected", "Invalid x coordinate: ${parts[0]}")
        val y = parts[1].trim().toIntOrNull()
            ?: return ActionResult.Failed("input_rejected", "Invalid y coordinate: ${parts[1]}")

        val path = Path().apply { moveTo(x.toFloat(), y.toFloat()) }
        // Use a human-like tap duration so WebView surfaces reliably register the gesture.
        val stroke = GestureDescription.StrokeDescription(path, 0L, 80L)
        val gesture = GestureDescription.Builder().addStroke(stroke).build()

        val latch = CountDownLatch(1)
        var succeeded = false
        val dispatched = service.dispatchGesture(
            gesture,
            object : AccessibilityService.GestureResultCallback() {
                override fun onCompleted(gestureDescription: GestureDescription) {
                    succeeded = true
                    latch.countDown()
                }
                override fun onCancelled(gestureDescription: GestureDescription) {
                    latch.countDown()
                }
            },
            null,
        )
        if (!dispatched) {
            return ActionResult.Failed("input_rejected", "Coordinate tap not dispatched at $x,$y")
        }
        if (!latch.await(2, TimeUnit.SECONDS)) {
            // Some WebView-backed surfaces on Waydroid never invoke gesture callbacks
            // even though the tap is actually applied. Treat this as best-effort success
            // so higher-level workflow retries can progress based on resulting UI state.
            Log.w(TAG, "Coordinate tap callback timed out at $x,$y; assuming delivered")
            return ActionResult.Ok
        }
        return if (succeeded) ActionResult.Ok
        else ActionResult.Failed("input_rejected", "Coordinate tap cancelled at $x,$y")
    }

    /**
     * Dispatches a swipe gesture from (startX, startY) to (endX, endY) using [GestureDescription].
     * Requires API 24+ (minSdk = 26, always satisfied).
     */
    private fun dispatchSwipe(action: AutomationAction.Swipe): ActionResult {
        val path = Path().apply {
            moveTo(action.startX.toFloat(), action.startY.toFloat())
            lineTo(action.endX.toFloat(), action.endY.toFloat())
        }
        val stroke = GestureDescription.StrokeDescription(path, 0L, action.durationMs)
        val gesture = GestureDescription.Builder().addStroke(stroke).build()

        val latch = CountDownLatch(1)
        var succeeded = false
        val dispatched = service.dispatchGesture(
            gesture,
            object : AccessibilityService.GestureResultCallback() {
                override fun onCompleted(gestureDescription: GestureDescription) {
                    succeeded = true
                    latch.countDown()
                }
                override fun onCancelled(gestureDescription: GestureDescription) {
                    latch.countDown()
                }
            },
            null,
        )
        if (!dispatched) {
            return ActionResult.Failed(
                "input_rejected",
                "Swipe gesture not dispatched (${action.startX},${action.startY})→(${action.endX},${action.endY})",
            )
        }
        if (!latch.await(action.durationMs + 2_000L, TimeUnit.MILLISECONDS)) {
            Log.w(TAG, "Swipe callback timed out; assuming delivered")
            return ActionResult.Ok
        }
        return if (succeeded) ActionResult.Ok
        else ActionResult.Failed(
            "input_rejected",
            "Swipe gesture cancelled (${action.startX},${action.startY})→(${action.endX},${action.endY})",
        )
    }

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
     * Accessibility labels are often nested inside a clickable row container.
     * For text-based actions we keep selector resolution simple and only climb
     * at execution time when the requested action needs an actionable ancestor.
     */
    private fun findSelfOrAncestor(
        node: AccessibilityNodeInfo,
        predicate: (AccessibilityNodeInfo) -> Boolean,
    ): AccessibilityNodeInfo? {
        if (predicate(node)) return node

        var current = node
        while (true) {
            val parent = current.parent ?: return null
            if (current !== node) current.recycle()
            if (predicate(parent)) return parent
            current = parent
        }
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

    private fun findNodeByContentDescription(root: AccessibilityNodeInfo, contentDescription: String): AccessibilityNodeInfo? {
        if (root.contentDescription?.toString() == contentDescription) return root
        for (i in 0 until root.childCount) {
            val child = root.getChild(i) ?: continue
            val result = findNodeByContentDescription(child, contentDescription)
            if (result != null) {
                if (result !== child) child.recycle()
                return result
            }
            child.recycle()
        }
        return null
    }

    private fun findNodeByText(root: AccessibilityNodeInfo, text: String): AccessibilityNodeInfo? {
        if (root.text?.toString() == text) return root
        for (i in 0 until root.childCount) {
            val child = root.getChild(i) ?: continue
            val result = findNodeByText(child, text)
            if (result != null) {
                if (result !== child) child.recycle()
                return result
            }
            child.recycle()
        }
        return null
    }

    private fun findNodeByResourceId(root: AccessibilityNodeInfo, resourceId: String): AccessibilityNodeInfo? {
        if (root.viewIdResourceName == resourceId) return root
        for (i in 0 until root.childCount) {
            val child = root.getChild(i) ?: continue
            val result = findNodeByResourceId(child, resourceId)
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
