package com.autosdk.agent.agent.script

import com.autosdk.agent.action.AutomationAction
import com.autosdk.agent.action.Selector
import com.autosdk.agent.action.SelectorKind
import com.autosdk.agent.observation.UiSnapshot
import org.mozilla.javascript.Context
import org.mozilla.javascript.NativeObject
import org.mozilla.javascript.Scriptable
import org.mozilla.javascript.ScriptableObject

/**
 * Shared helpers used by [JsAutomationBridge] bridge functions.
 *
 * Extracted to keep [JsAutomationBridge] focused on function registration
 * and to allow independent unit testing of parsing/building logic.
 */
/**
 * Typed bridge error for Kotlin-side pattern matching (metrics, logging).
 * The JS-visible message format is always `"function: detail"`.
 */
sealed class BridgeError(val function: String, val detail: String) {
    class InvalidArgs(fn: String, detail: String) : BridgeError(fn, detail)
    class ActionFailed(fn: String, detail: String) : BridgeError(fn, detail)
    class Unavailable(fn: String, detail: String) : BridgeError(fn, detail)

    fun message(): String = "$function: $detail"
}

internal object JsBridgeHelpers {

    /**
     * Throws a Rhino EcmaError with [message] — stops JS execution and lets the script catch it.
     * Returns [Nothing] so Kotlin infers the correct type for `?:` expressions.
     */
    fun scriptError(message: String): Nothing {
        Context.reportRuntimeError(message) // throws EcmaError
        error(message) // unreachable; satisfies Kotlin's Nothing type
    }

    /** Typed overload — formats [BridgeError.message] and throws a Rhino EcmaError. */
    fun scriptError(error: BridgeError): Nothing = scriptError(error.message())

    fun buildResultObject(cx: Context, scope: Scriptable, vararg pairs: Pair<String, Any?>): NativeObject {
        val obj = cx.newObject(scope) as NativeObject
        for ((k, v) in pairs) {
            ScriptableObject.putProperty(obj, k, v ?: Context.getUndefinedValue())
        }
        return obj
    }

    fun parseSelectorFromNative(obj: NativeObject): Selector? {
        val kind = obj.get("kind", obj)?.toString() ?: return null
        val value = obj.get("value", obj)?.toString() ?: return null
        val selectorKind = when (kind) {
            "text" -> SelectorKind.TEXT
            "resource_id" -> SelectorKind.RESOURCE_ID
            "semantic_key" -> SelectorKind.SEMANTIC_KEY
            "target_id" -> SelectorKind.TARGET_ID
            "content_desc" -> SelectorKind.CONTENT_DESC
            "bounds" -> SelectorKind.BOUNDS
            "package_name" -> SelectorKind.PACKAGE_NAME
            "coordinate" -> SelectorKind.COORDINATE
            else -> return null
        }
        return Selector(selectorKind, value)
    }

    fun parseActionFromNative(obj: NativeObject): AutomationAction? {
        val kind = obj.get("kind", obj)?.toString() ?: return null
        return when (kind) {
            "click" -> {
                val targetObj = obj.get("target", obj) as? NativeObject ?: return null
                val selector = parseSelectorFromNative(targetObj) ?: return null
                AutomationAction.Click(selector)
            }
            "long_press" -> {
                val targetObj = obj.get("target", obj) as? NativeObject ?: return null
                val selector = parseSelectorFromNative(targetObj) ?: return null
                AutomationAction.LongPress(selector)
            }
            "input_text" -> {
                val targetObj = obj.get("target", obj) as? NativeObject ?: return null
                val selector = parseSelectorFromNative(targetObj) ?: return null
                val text = obj.get("inputText", obj)?.toString() ?: ""
                AutomationAction.InputText(selector, text)
            }
            "open_app" -> {
                val targetObj = obj.get("target", obj) as? NativeObject ?: return null
                val pkg = targetObj.get("value", targetObj)?.toString() ?: return null
                AutomationAction.OpenApp(pkg)
            }
            "open_intent" -> {
                val intentAction = obj.get("intentAction", obj)?.toString() ?: return null
                val pkg = obj.get("package", obj)?.toString()
                AutomationAction.OpenIntent(intentAction, pkg)
            }
            "swipe" -> {
                val startX = (obj.get("startX", obj) as? Number)?.toInt() ?: return null
                val startY = (obj.get("startY", obj) as? Number)?.toInt() ?: return null
                val endX = (obj.get("endX", obj) as? Number)?.toInt() ?: return null
                val endY = (obj.get("endY", obj) as? Number)?.toInt() ?: return null
                val durationMs = (obj.get("durationMs", obj) as? Number)?.toLong() ?: 300L
                AutomationAction.Swipe(startX, startY, endX, endY, durationMs)
            }
            "back" -> AutomationAction.Back
            "home" -> AutomationAction.Home
            else -> null
        }
    }

    fun snapshotContains(snapshot: UiSnapshot, selector: Selector): Boolean =
        snapshot.targets.any { target ->
            when (selector.kind) {
                SelectorKind.TEXT -> target.text == selector.value
                SelectorKind.RESOURCE_ID -> target.resourceId == selector.value
                SelectorKind.SEMANTIC_KEY -> target.semanticKey == selector.value
                SelectorKind.CONTENT_DESC -> target.contentDesc == selector.value
                SelectorKind.PACKAGE_NAME -> target.packageName == selector.value
                else -> false
            }
        }
}
