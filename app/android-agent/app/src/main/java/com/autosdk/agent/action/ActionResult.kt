package com.autosdk.agent.action

/**
 * Result of an [ActionExecutor.execute] call.
 *
 * Using a sealed type instead of Boolean lets the agent surface the correct
 * failure class to the server, which in turn uses it for recovery decisions.
 * Collapsing all failures to "target not found" makes automated recovery
 * impossible — a disabled button needs different handling than a missing one.
 */
sealed interface ActionResult {
    data object Ok : ActionResult

    /**
     * @param failureClass One of the ADR failure class strings:
     *   `target_not_found`, `target_not_actionable`, `capability_unavailable`,
     *   `input_rejected`, `internal_error`.
     */
    data class Failed(val failureClass: String, val reason: String) : ActionResult
}
