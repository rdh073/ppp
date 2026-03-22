package com.autosdk.agent.agent

import com.autosdk.agent.action.AutomationAction
import com.autosdk.agent.action.Selector
import com.autosdk.agent.action.SelectorKind
import com.autosdk.agent.agent.script.JsBridgeHelpers
import com.autosdk.agent.observation.UiSemanticState
import com.autosdk.agent.observation.UiSnapshot
import com.autosdk.agent.observation.UiTarget
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.mozilla.javascript.Context
import org.mozilla.javascript.ContextFactory
import org.mozilla.javascript.NativeObject
import org.mozilla.javascript.ScriptableObject

class JsBridgeHelpersTest {

    // ---- parseSelectorFromNative ----

    @Test
    fun `parseSelectorFromNative returns Selector for valid text selector`() {
        withRhino { cx, scope ->
            val obj = nativeObj(cx, scope, "kind" to "text", "value" to "OK")
            val selector = JsBridgeHelpers.parseSelectorFromNative(obj)
            assertNotNull(selector)
            assertEquals(SelectorKind.TEXT, selector!!.kind)
            assertEquals("OK", selector.value)
        }
    }

    @Test
    fun `parseSelectorFromNative returns Selector for resource_id`() {
        withRhino { cx, scope ->
            val obj = nativeObj(cx, scope, "kind" to "resource_id", "value" to "com.app:id/btn")
            val selector = JsBridgeHelpers.parseSelectorFromNative(obj)
            assertNotNull(selector)
            assertEquals(SelectorKind.RESOURCE_ID, selector!!.kind)
        }
    }

    @Test
    fun `parseSelectorFromNative returns null for missing kind`() {
        withRhino { cx, scope ->
            val obj = nativeObj(cx, scope, "value" to "OK")
            assertNull(JsBridgeHelpers.parseSelectorFromNative(obj))
        }
    }

    @Test
    fun `parseSelectorFromNative returns null for unknown kind`() {
        withRhino { cx, scope ->
            val obj = nativeObj(cx, scope, "kind" to "magic", "value" to "x")
            assertNull(JsBridgeHelpers.parseSelectorFromNative(obj))
        }
    }

    // ---- parseActionFromNative ----

    @Test
    fun `parseActionFromNative parses click action`() {
        withRhino { cx, scope ->
            val target = nativeObj(cx, scope, "kind" to "text", "value" to "Submit")
            val obj = nativeObj(cx, scope, "kind" to "click", "target" to target)
            val action = JsBridgeHelpers.parseActionFromNative(obj)
            assertTrue(action is AutomationAction.Click)
            assertEquals(SelectorKind.TEXT, (action as AutomationAction.Click).selector.kind)
            assertEquals("Submit", action.selector.value)
        }
    }

    @Test
    fun `parseActionFromNative parses back action`() {
        withRhino { cx, scope ->
            val obj = nativeObj(cx, scope, "kind" to "back")
            assertEquals(AutomationAction.Back, JsBridgeHelpers.parseActionFromNative(obj))
        }
    }

    @Test
    fun `parseActionFromNative parses home action`() {
        withRhino { cx, scope ->
            val obj = nativeObj(cx, scope, "kind" to "home")
            assertEquals(AutomationAction.Home, JsBridgeHelpers.parseActionFromNative(obj))
        }
    }

    @Test
    fun `parseActionFromNative returns null for unknown kind`() {
        withRhino { cx, scope ->
            val obj = nativeObj(cx, scope, "kind" to "teleport")
            assertNull(JsBridgeHelpers.parseActionFromNative(obj))
        }
    }

    // ---- snapshotContains ----

    @Test
    fun `snapshotContains returns true when text target matches`() {
        val snapshot = testSnapshot(listOf(testTarget(text = "Submit")))
        assertTrue(JsBridgeHelpers.snapshotContains(snapshot, Selector(SelectorKind.TEXT, "Submit")))
    }

    @Test
    fun `snapshotContains returns false when no target matches`() {
        val snapshot = testSnapshot(listOf(testTarget(text = "Cancel")))
        assertEquals(false, JsBridgeHelpers.snapshotContains(snapshot, Selector(SelectorKind.TEXT, "Submit")))
    }

    @Test
    fun `snapshotContains matches by resource_id`() {
        val snapshot = testSnapshot(listOf(testTarget(resourceId = "com.app:id/btn")))
        assertTrue(JsBridgeHelpers.snapshotContains(snapshot, Selector(SelectorKind.RESOURCE_ID, "com.app:id/btn")))
    }

    // ---- buildResultObject ----

    @Test
    fun `buildResultObject creates NativeObject with pairs`() {
        withRhino { cx, scope ->
            val obj = JsBridgeHelpers.buildResultObject(cx, scope, "ok" to true, "count" to 3)
            assertEquals(true, obj.get("ok", obj))
            assertEquals(3, obj.get("count", obj))
        }
    }

    // ---- test helpers ----

    private fun withRhino(block: (Context, ScriptableObject) -> Unit) {
        val factory = ContextFactory()
        val cx = factory.enterContext()
        try {
            cx.optimizationLevel = -1
            val scope = cx.initStandardObjects()
            block(cx, scope)
        } finally {
            Context.exit()
        }
    }

    private fun nativeObj(cx: Context, scope: ScriptableObject, vararg pairs: Pair<String, Any?>): NativeObject {
        val obj = cx.newObject(scope) as NativeObject
        for ((k, v) in pairs) {
            ScriptableObject.putProperty(obj, k, v ?: Context.getUndefinedValue())
        }
        return obj
    }

    private fun testTarget(
        text: String? = null,
        resourceId: String? = null,
    ) = UiTarget(
        targetId = "t1",
        role = null,
        uiRole = "text",
        label = null,
        semanticKey = null,
        formKey = null,
        text = text,
        contentDesc = null,
        resourceId = resourceId,
        packageName = null,
        bounds = intArrayOf(0, 0, 100, 50),
        actionable = true,
        enabled = true,
        checked = null,
        selected = false,
        scrollable = false,
        focused = false,
        password = false,
    )

    private fun testSnapshot(targets: List<UiTarget>) = UiSnapshot(
        snapshotId = "snap-1",
        deviceId = "dev-1",
        packageName = "com.test",
        activityName = "MainActivity",
        screenState = null,
        focusedTargetId = null,
        semantic = UiSemanticState(
            activeUiKey = "com.test.main",
            baseScreenKey = "com.test.main",
            overlayKey = null,
            uiReady = true,
            semanticDigest = "abc",
            focusedTargetKey = null,
            forms = emptyList(),
            buttons = emptyList(),
        ),
        capturedAt = "2026-01-01T00:00:00Z",
        targets = targets,
    )
}
