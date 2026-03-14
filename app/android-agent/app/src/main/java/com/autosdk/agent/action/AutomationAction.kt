package com.autosdk.agent.action

enum class ScrollDirection { FORWARD, BACKWARD }

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
    /** [selector] is optional; when absent the agent scrolls the foreground window. */
    data class Scroll(
        val selector: Selector?,
        val direction: ScrollDirection = ScrollDirection.FORWARD,
    ) : AutomationAction
    data object Home : AutomationAction
    data object Back : AutomationAction
    data object Wake : AutomationAction
    data class OpenApp(val packageName: String) : AutomationAction
    data object CloseApp : AutomationAction
    data object Screenshot : AutomationAction
}
