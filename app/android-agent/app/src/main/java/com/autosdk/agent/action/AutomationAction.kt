package com.autosdk.agent.action

enum class ScrollDirection { FORWARD, BACKWARD }

/** One field entry in a [AutomationAction.FillForm] action. */
data class FieldFill(val selector: Selector, val value: String)

/**
 * Mirrors the AutomationActionKind discriminated union in the contracts package.
 * All targeted actions carry a [Selector] rather than a raw node reference, so
 * the agent resolves them against the live accessibility tree at execution time.
 */
sealed interface AutomationAction {
    data class Click(val selector: Selector) : AutomationAction
    data class LongPress(val selector: Selector) : AutomationAction
    data class InputText(val selector: Selector, val value: String) : AutomationAction
    data class DeleteText(val selector: Selector) : AutomationAction
    data class Drag(val selector: Selector) : AutomationAction
    /** Swipe gesture from (startX, startY) to (endX, endY) in screen coordinates. */
    data class Swipe(
        val startX: Int,
        val startY: Int,
        val endX: Int,
        val endY: Int,
        val durationMs: Long = 300L,
    ) : AutomationAction
    /** [selector] is optional; when absent the agent scrolls the foreground window. */
    data class Scroll(
        val selector: Selector?,
        val direction: ScrollDirection = ScrollDirection.FORWARD,
    ) : AutomationAction
    data object Home : AutomationAction
    data object Back : AutomationAction
    data object Wake : AutomationAction
    data class OpenApp(val packageName: String) : AutomationAction
    data class OpenIntent(val intentAction: String, val packageName: String?) : AutomationAction
    data object CloseApp : AutomationAction
    data object Screenshot : AutomationAction
    /**
     * Fills multiple input fields in a single device round-trip.
     * The agent iterates [fields] in order, setting text on each node.
     * Fails fast on the first field that cannot be resolved or set.
     */
    data class FillForm(val fields: List<FieldFill>) : AutomationAction
}
