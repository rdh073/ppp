package com.autosdk.agent.observation

/**
 * A normalized representation of a single UI node captured from the accessibility tree.
 *
 * State fields (enabled, checked, scrollable, …) are included so the server can
 * classify failures correctly without a second round-trip. For example:
 *   - [enabled] = false → `target_not_actionable` (not `target_not_found`)
 *   - [scrollable] = true → the server knows it can issue scroll commands to reveal offscreen targets
 *   - [checked] is non-null → the node is a checkable widget (toggle, checkbox, radio)
 *
 * [contentDesc] is kept for internal selector resolution but is not emitted on
 * the wire (not in the contracts UiTargetSchema).
 */
data class UiTarget(
    val targetId: String,
    val role: String?,
    /** Semantic role: button|input|checkbox|switch|radio|loading|text|image|list|container */
    val uiRole: String,
    /** contentDescription for inputs/checkboxes/switches/radios; null for other roles. */
    val label: String?,
    /** Stable semantic selector for workflow actions, e.g. `form.primary.email` or `button.save`. */
    val semanticKey: String?,
    /** Parent semantic form identifier when the target belongs to the active form. */
    val formKey: String?,
    val text: String?,
    val contentDesc: String?,
    val resourceId: String?,
    val packageName: String?,
    /** [left, top, right, bottom] in screen pixels. */
    val bounds: IntArray,
    val actionable: Boolean,
    val enabled: Boolean,
    /** Non-null only for checkable widgets (checkbox, toggle, radio button). */
    val checked: Boolean?,
    val selected: Boolean,
    val scrollable: Boolean,
    val focused: Boolean,
    val password: Boolean,
)

/**
 * A committed snapshot of the device UI state, aligned with the contracts UiSnapshot interface.
 *
 * [capturedAt] is an ISO-8601 instant string.
 * [screenState] is populated by [SnapshotBuilder]: "loading", "dialog", or "ready".
 * [focusedTargetId] is the [UiTarget.targetId] of the currently focused node, or null.
 * [semantic] is the workflow-facing semantic projection derived from the raw accessibility tree.
 */
data class UiSnapshot(
    val snapshotId: String,
    val deviceId: String,
    val packageName: String?,
    val activityName: String?,
    val screenState: String?,
    val focusedTargetId: String?,
    val semantic: UiSemanticState,
    val capturedAt: String,
    val targets: List<UiTarget>,
)
