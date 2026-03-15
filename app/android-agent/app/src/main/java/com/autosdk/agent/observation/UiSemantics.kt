package com.autosdk.agent.observation

import java.security.MessageDigest
import java.util.Locale

data class UiFormState(
    val formKey: String,
    val fieldKeys: List<String>,
    val focusedFieldKey: String?,
    val ready: Boolean,
)

data class UiButtonState(
    val buttonKey: String,
    val enabled: Boolean,
    val visible: Boolean,
    val primary: Boolean,
)

data class UiSemanticState(
    val activeUiKey: String,
    val baseScreenKey: String,
    val overlayKey: String?,
    val uiReady: Boolean,
    val semanticDigest: String,
    val focusedTargetKey: String?,
    val forms: List<UiFormState>,
    val buttons: List<UiButtonState>,
)

internal data class SemanticProjection(
    val targets: List<UiTarget>,
    val semantic: UiSemanticState,
)

/**
 * Classifies a UI node into a semantic role string based on its class name and state flags.
 *
 * Priority order matters: ProgressBar extends View, some SDKs extend Button.
 * Extracted as a top-level function so it can be tested without Robolectric.
 */
internal fun classifyUiRole(
    className: String?,
    isEditable: Boolean,
    isClickable: Boolean,
    isCheckable: Boolean,
    isScrollable: Boolean,
): String {
    val cn =
        className ?: return when {
            isEditable -> "input"
            isCheckable -> "checkbox"
            isScrollable -> "scroll_container"
            isClickable -> "button"
            else -> "container"
        }
    if (cn.contains("ProgressBar") || cn.contains("CircularProgress")) return "loading"
    if (isEditable || cn.endsWith("EditText") || cn.contains("TextInputEditText")) return "input"
    if (cn.endsWith("CheckBox")) return "checkbox"
    if (cn.endsWith("Switch") || cn.contains("SwitchCompat") || cn.contains("MaterialSwitch")) return "switch"
    if (cn.endsWith("RadioButton")) return "radio"
    if (cn.endsWith("ToggleButton")) return "toggle"
    if (cn.endsWith("Button") || cn.contains("Chip") || cn.contains("FloatingActionButton")) return "button"
    if (isClickable && cn.endsWith("TextView")) return "button"
    if (cn.endsWith("TextView")) return "text"
    if (cn.endsWith("ImageView") || cn.endsWith("ImageButton")) return "image"
    if (cn.endsWith("RecyclerView") || cn.endsWith("ListView") || cn.endsWith("GridView")) return "list"
    if (isScrollable) return "scroll_container"
    return "container"
}

internal fun buildSemanticProjection(
    rawTargets: List<UiTarget>,
    packageName: String?,
    activityName: String?,
    screenState: String,
): SemanticProjection {
    val baseScreenKey = deriveBaseScreenKey(packageName, activityName, rawTargets)
    val overlayKey = if (screenState == "dialog") deriveOverlayKey(rawTargets) else null
    val activeUiKey = overlayKey ?: baseScreenKey
    val uiReady = screenState != "loading" && rawTargets.isNotEmpty()

    val inputTargets = rawTargets.filter { it.uiRole == "input" }
    val formKey = if (inputTargets.isNotEmpty()) deriveFormKey(inputTargets) else null
    val assignedFormKeys = rawTargets.map { target ->
        when {
            target.uiRole == "input" -> formKey
            target.uiRole == "button" && formKey != null && looksLikeSubmitAction(anchorForTarget(target)) -> formKey
            else -> null
        }
    }

    val duplicateCounts = mutableMapOf<String, Int>()
    val enrichedTargets =
        rawTargets.mapIndexed { index, target ->
            val targetFormKey = assignedFormKeys[index]
            val baseKey = buildSemanticKey(target, targetFormKey)
            val uniqueKey = uniquifySemanticKey(baseKey, duplicateCounts)
            target.copy(
                semanticKey = uniqueKey,
                formKey = targetFormKey,
            )
        }

    val focusedTargetKey = enrichedTargets.firstOrNull { it.focused }?.semanticKey
    val buttons = buildButtonStates(enrichedTargets)
    val forms = buildFormStates(enrichedTargets, formKey, focusedTargetKey, uiReady, buttons)
    val semantic =
        UiSemanticState(
            activeUiKey = activeUiKey,
            baseScreenKey = baseScreenKey,
            overlayKey = overlayKey,
            uiReady = uiReady,
            semanticDigest = computeSemanticDigest(activeUiKey, baseScreenKey, overlayKey, screenState, focusedTargetKey, enrichedTargets),
            focusedTargetKey = focusedTargetKey,
            forms = forms,
            buttons = buttons,
        )

    return SemanticProjection(targets = enrichedTargets, semantic = semantic)
}

private fun deriveBaseScreenKey(
    packageName: String?,
    activityName: String?,
    targets: List<UiTarget>,
): String {
    val packageKey = derivePackageKey(packageName)
    val activityKey =
        activityName
            ?.substringAfterLast('.')
            ?.removeSuffix("Activity")
            ?.removeSuffix("Fragment")
            ?.removeSuffix("Dialog")
            ?.removeSuffix("Screen")
            ?.let(::normalizeKeyPart)

    return when {
        !packageKey.isNullOrBlank() && !activityKey.isNullOrBlank() -> "$packageKey.$activityKey"
        !packageKey.isNullOrBlank() -> {
            val anchor = targets.firstNotNullOfOrNull(::findVisibleAnchor)
            if (anchor != null) "$packageKey.$anchor" else packageKey
        }

        !activityKey.isNullOrBlank() -> activityKey
        else -> "screen.${targets.firstNotNullOfOrNull(::findVisibleAnchor) ?: "unknown"}"
    }
}

private fun derivePackageKey(packageName: String?): String? {
    val segments =
        packageName
            ?.split('.')
            ?.map(::normalizeKeyPart)
            ?.filter { it.isNotBlank() }
            .orEmpty()
    if (segments.isEmpty()) {
        return null
    }

    // Generic Android/build-variant suffixes that add no domain signal.
    // When the last package segment matches one of these, the parent segment
    // is used as the stable key instead (e.g. "com.example.app" → "example").
    val genericSuffixes = setOf("app", "application", "android", "mobile", "debug", "release", "prod", "qa", "dev", "staging")
    val lastSegment = segments.last()
    return if (lastSegment in genericSuffixes && segments.size >= 2) {
        segments[segments.lastIndex - 1]
    } else {
        lastSegment
    }
}

private fun deriveOverlayKey(targets: List<UiTarget>): String {
    val anchor = targets.firstNotNullOfOrNull(::findVisibleAnchor)
    return if (anchor != null) "dialog.$anchor" else "dialog.system"
}

private fun deriveFormKey(inputs: List<UiTarget>): String {
    val prefixes =
        inputs
            .mapNotNull { target ->
                val normalized = target.resourceId?.substringAfterLast('/')?.let(::normalizeKeyPart) ?: return@mapNotNull null
                normalized.substringBeforeLast('_', missingDelimiterValue = "")
                    .takeIf { it.isNotBlank() }
            }
            .groupingBy { it }
            .eachCount()

    val stablePrefix =
        prefixes
            .filterValues { it >= 2 }
            .maxByOrNull { it.value }
            ?.key

    return if (!stablePrefix.isNullOrBlank()) {
        "form.$stablePrefix"
    } else {
        "form.primary"
    }
}

private fun buildSemanticKey(
    target: UiTarget,
    formKey: String?,
): String? {
    val anchor = anchorForTarget(target)
    return when (target.uiRole) {
        "input" ->
            if (formKey != null) {
                "$formKey.${stripFormPrefix(anchor, formKey)}"
            } else {
                "input.$anchor"
            }

        "button" ->
            if (formKey != null && looksLikeSubmitAction(anchor)) {
                "$formKey.submit"
            } else {
                "button.$anchor"
            }

        "checkbox", "switch", "radio", "toggle", "loading", "list", "scroll_container", "text", "image" ->
            "${target.uiRole}.$anchor"

        else ->
            if (anchor == "container") {
                null
            } else {
                "${target.uiRole}.$anchor"
            }
    }
}

private fun buildButtonStates(targets: List<UiTarget>): List<UiButtonState> {
    val primaryKey =
        targets
            .filter { it.uiRole == "button" && !it.semanticKey.isNullOrBlank() }
            .firstOrNull { looksLikePrimaryButton(it) }
            ?.semanticKey
            ?: targets.firstOrNull { it.uiRole == "button" && it.enabled && !it.semanticKey.isNullOrBlank() }?.semanticKey

    return targets
        .filter { it.uiRole == "button" && !it.semanticKey.isNullOrBlank() }
        .map { target ->
            UiButtonState(
                buttonKey = target.semanticKey!!,
                enabled = target.enabled,
                visible = true,
                primary = target.semanticKey == primaryKey,
            )
        }
}

private fun buildFormStates(
    targets: List<UiTarget>,
    formKey: String?,
    focusedTargetKey: String?,
    uiReady: Boolean,
    buttons: List<UiButtonState>,
): List<UiFormState> {
    if (formKey == null) {
        return emptyList()
    }

    val fields =
        targets
            .filter { it.uiRole == "input" && it.formKey == formKey && !it.semanticKey.isNullOrBlank() }
            .map { it.semanticKey!! }
    if (fields.isEmpty()) {
        return emptyList()
    }

    val submitButtons = buttons.filter { it.buttonKey.startsWith("$formKey.") }
    val inputsEnabled = targets.filter { it.uiRole == "input" && it.formKey == formKey }.all { it.enabled }
    return listOf(
        UiFormState(
            formKey = formKey,
            fieldKeys = fields,
            focusedFieldKey = focusedTargetKey?.takeIf { it in fields },
            ready = uiReady && inputsEnabled && (submitButtons.isEmpty() || submitButtons.any { it.enabled }),
        ),
    )
}

private fun computeSemanticDigest(
    activeUiKey: String,
    baseScreenKey: String,
    overlayKey: String?,
    screenState: String,
    focusedTargetKey: String?,
    targets: List<UiTarget>,
): String {
    val parts =
        buildList {
            add("activeUiKey=$activeUiKey")
            add("baseScreenKey=$baseScreenKey")
            add("overlayKey=${overlayKey ?: ""}")
            add("screenState=$screenState")
            add("focusedTargetKey=${focusedTargetKey ?: ""}")
            targets
                .asSequence()
                .filter { !it.semanticKey.isNullOrBlank() }
                .sortedBy { it.semanticKey }
                .forEach { target ->
                    add(
                        listOf(
                            target.semanticKey,
                            target.enabled.toString(),
                            target.checked?.toString() ?: "null",
                            target.selected.toString(),
                            target.focused.toString(),
                        ).joinToString(":"),
                    )
                }
        }

    return sha1(parts.joinToString("|"))
}

private fun anchorForTarget(target: UiTarget): String {
    val resourceAnchor = target.resourceId?.substringAfterLast('/')?.let(::normalizeKeyPart)
    if (!resourceAnchor.isNullOrBlank()) {
        return resourceAnchor
    }

    return sequenceOf(target.label, target.text, target.contentDesc, target.role, target.uiRole)
        .mapNotNull { value -> value?.let(::normalizeKeyPart) }
        .firstOrNull()
        ?: "container"
}

private fun stripFormPrefix(
    anchor: String,
    formKey: String,
): String {
    val stem = formKey.substringAfterLast('.')
    return when {
        anchor.startsWith("${stem}_") -> anchor.removePrefix("${stem}_")
        anchor.startsWith("${stem}.") -> anchor.removePrefix("${stem}.")
        anchor == stem -> "field"
        else -> anchor
    }
}

// Heuristic for primary form-submit buttons — keep conservative to avoid false positives.
// A button whose anchor contains any of these tokens is treated as a submit action.
private fun looksLikeSubmitAction(anchor: String): Boolean =
    listOf("submit", "save", "continue", "next", "done", "confirm", "login", "sign_in", "ok", "allow")
        .any { token -> anchor.contains(token) }

private fun looksLikePrimaryButton(target: UiTarget): Boolean {
    val anchor = anchorForTarget(target)
    return target.enabled && looksLikeSubmitAction(anchor)
}

private fun findVisibleAnchor(target: UiTarget): String? =
    sequenceOf(target.text, target.label, target.contentDesc, target.resourceId?.substringAfterLast('/'))
        .mapNotNull { value -> value?.let(::normalizeKeyPart) }
        .firstOrNull()

private fun uniquifySemanticKey(
    key: String?,
    counts: MutableMap<String, Int>,
): String? {
    if (key.isNullOrBlank()) {
        return null
    }

    val next = (counts[key] ?: 0) + 1
    counts[key] = next
    return if (next == 1) key else "${key}_$next"
}

private fun normalizeKeyPart(raw: String): String {
    val decamel = raw.replace(Regex("([a-z0-9])([A-Z])"), "$1_$2")
    return decamel
        .lowercase(Locale.US)
        .replace(Regex("[^a-z0-9]+"), "_")
        .trim('_')
        .ifBlank { "unknown" }
}

private fun sha1(input: String): String {
    val digest = MessageDigest.getInstance("SHA-1").digest(input.toByteArray())
    return digest.joinToString(separator = "") { byte -> "%02x".format(byte) }
}
