package com.autosdk.agent.agent

import com.autosdk.agent.action.ActionResult
import com.autosdk.agent.action.AutomationAction
import com.autosdk.agent.action.Selector
import com.autosdk.agent.action.SelectorKind
import com.autosdk.agent.agent.script.JsAutomationBridge
import com.autosdk.agent.agent.script.JsRuntime
import com.autosdk.agent.observation.UiSemanticState
import com.autosdk.agent.observation.UiSnapshot
import com.autosdk.agent.observation.UiTarget
import okhttp3.OkHttpClient
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

class JsRuntimeTest {

    private fun makeRuntime(
        snapshot: UiSnapshot? = null,
        executeResult: ActionResult = ActionResult.Ok,
    ): Pair<JsRuntime, JsFakeAutomationDriver> {
        val driver = JsFakeAutomationDriver(executeResult = executeResult, snapshot = snapshot)
        val bridge = JsAutomationBridge(
            snapshotBuilder = { driver.snapshot },
            automationDriver = driver,
            okHttpClient = OkHttpClient(),
        )
        return JsRuntime(bridge) to driver
    }

    // ---- basic execution ----

    @Test
    fun `script returning plain object emits output`() {
        val (runtime, _) = makeRuntime()
        val result = runtime.execute("return { ok: true, count: 3 }", emptyMap(), 5_000L)
        assertTrue(result.isSuccess)
        val out = result.getOrThrow().output
        assertEquals(true, out["ok"])
        assertEquals(3.0, out["count"])
    }

    @Test
    fun `params injected into script scope`() {
        val (runtime, _) = makeRuntime()
        val result = runtime.execute("return { name: params.name }", mapOf("name" to "Alice"), 5_000L)
        assertTrue(result.isSuccess)
        assertEquals("Alice", result.getOrThrow().output["name"])
    }

    @Test
    fun `script with no return value produces empty output`() {
        val (runtime, _) = makeRuntime()
        val result = runtime.execute("var x = 1 + 1;", emptyMap(), 5_000L)
        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().output.isEmpty())
    }

    @Test
    fun `thrown error propagates as failure`() {
        val (runtime, _) = makeRuntime()
        val result = runtime.execute("throw new Error('boom');", emptyMap(), 5_000L)
        assertFalse(result.isSuccess)
        assertTrue(result.exceptionOrNull()?.message?.contains("boom") == true)
    }

    @Test
    fun `syntax error propagates as failure`() {
        val (runtime, _) = makeRuntime()
        val result = runtime.execute("{{{{ invalid js", emptyMap(), 5_000L)
        assertFalse(result.isSuccess)
    }

    @Test
    fun `durationMs is non-negative`() {
        val (runtime, _) = makeRuntime()
        val result = runtime.execute("return {};", emptyMap(), 5_000L)
        assertTrue(result.getOrThrow().durationMs >= 0)
    }

    // ---- log bridge ----

    @Test
    fun `log() appends to bridge logs`() {
        val (runtime, _) = makeRuntime()
        val result = runtime.execute("log('hello'); log('world'); return {};", emptyMap(), 5_000L)
        assertTrue(result.isSuccess)
        assertEquals(listOf("hello", "world"), result.getOrThrow().logs)
    }

    // ---- tap bridge ----

    @Test
    fun `tap() dispatches click action`() {
        val (runtime, driver) = makeRuntime()
        val result = runtime.execute(
            "tap({ kind: 'click', target: { kind: 'text', value: 'OK' } }); return { ok: true };",
            emptyMap(),
            5_000L,
        )
        assertTrue(result.isSuccess)
        assertNotNull(driver.lastAction)
        assertTrue(driver.lastAction is AutomationAction.Click)
        val click = driver.lastAction as AutomationAction.Click
        assertEquals(SelectorKind.TEXT, click.selector.kind)
        assertEquals("OK", click.selector.value)
    }

    @Test
    fun `tap() failure propagates as script error`() {
        val (runtime, _) = makeRuntime(executeResult = ActionResult.Failed("target_not_found", "not found"))
        val result = runtime.execute(
            "tap({ kind: 'click', target: { kind: 'text', value: 'Missing' } });",
            emptyMap(),
            5_000L,
        )
        assertFalse(result.isSuccess)
        assertTrue(result.exceptionOrNull()?.message?.contains("not found") == true)
    }

    // ---- waitFor bridge ----

    @Test
    fun `waitFor() returns true when target present in snapshot`() {
        val target = fakeTarget(text = "Submit")
        val snapshot = fakeSnapshot(listOf(target))
        val (runtime, _) = makeRuntime(snapshot = snapshot)
        val result = runtime.execute(
            "var found = waitFor({ kind: 'text', value: 'Submit' }, 1000); return { found: found };",
            emptyMap(),
            5_000L,
        )
        assertTrue(result.isSuccess)
        assertEquals(true, result.getOrThrow().output["found"])
    }

    @Test
    fun `waitFor() returns false when target absent`() {
        val snapshot = fakeSnapshot(emptyList())
        val (runtime, _) = makeRuntime(snapshot = snapshot)
        val result = runtime.execute(
            "var found = waitFor({ kind: 'text', value: 'Ghost' }, 300); return { found: found };",
            emptyMap(),
            5_000L,
        )
        assertTrue(result.isSuccess)
        assertEquals(false, result.getOrThrow().output["found"])
    }

    // ---- scroll / back / home bridges ----

    @Test
    fun `scroll() dispatches Scroll action`() {
        val (runtime, driver) = makeRuntime()
        val result = runtime.execute(
            "scroll(null, 'forward'); return { ok: true };",
            emptyMap(),
            5_000L,
        )
        assertTrue(result.isSuccess)
        assertTrue(driver.lastAction is AutomationAction.Scroll)
    }

    @Test
    fun `back() dispatches Back action`() {
        val (runtime, driver) = makeRuntime()
        val result = runtime.execute("back(); return { ok: true };", emptyMap(), 5_000L)
        assertTrue(result.isSuccess)
        assertEquals(AutomationAction.Back, driver.lastAction)
    }

    @Test
    fun `home() dispatches Home action`() {
        val (runtime, driver) = makeRuntime()
        val result = runtime.execute("home(); return { ok: true };", emptyMap(), 5_000L)
        assertTrue(result.isSuccess)
        assertEquals(AutomationAction.Home, driver.lastAction)
    }

    // ---- awaitEvent bridge ----

    @Test
    fun `awaitEvent() returns true when eventAwaiter resolves`() {
        val driver = JsFakeAutomationDriver()
        val bridge = JsAutomationBridge(
            snapshotBuilder = { null },
            automationDriver = driver,
            okHttpClient = okhttp3.OkHttpClient(),
            eventAwaiter = { _, _, _, _ -> true },
        )
        val runtime = JsRuntime(bridge)
        val result = runtime.execute(
            "var ok = awaitEvent('activity_created', {}, 5000); return { ok: ok };",
            emptyMap(),
            10_000L,
        )
        assertTrue(result.isSuccess)
        assertEquals(true, result.getOrThrow().output["ok"])
    }

    @Test
    fun `awaitEvent() returns false when eventAwaiter times out`() {
        val driver = JsFakeAutomationDriver()
        val bridge = JsAutomationBridge(
            snapshotBuilder = { null },
            automationDriver = driver,
            okHttpClient = okhttp3.OkHttpClient(),
            eventAwaiter = { _, _, _, _ -> false },
        )
        val runtime = JsRuntime(bridge)
        val result = runtime.execute(
            "var ok = awaitEvent('activity_created', {}, 100); return { ok: ok };",
            emptyMap(),
            5_000L,
        )
        assertTrue(result.isSuccess)
        assertEquals(false, result.getOrThrow().output["ok"])
    }

    // ---- input bridge ----

    @Test
    fun `input() dispatches InputText action`() {
        val (runtime, driver) = makeRuntime()
        val result = runtime.execute(
            "input({ kind: 'resource_id', value: 'com.app:id/field' }, 'hello'); return { ok: true };",
            emptyMap(),
            5_000L,
        )
        assertTrue(result.isSuccess)
        val action = driver.lastAction as? AutomationAction.InputText
        assertNotNull(action)
        assertEquals("hello", action!!.value)
        assertEquals(SelectorKind.RESOURCE_ID, action.selector.kind)
    }
}

// ---- test doubles ----

private class JsFakeAutomationDriver(
    val executeResult: ActionResult = ActionResult.Ok,
    val snapshot: UiSnapshot? = null,
) : AgentAutomationDriver {
    var lastAction: AutomationAction? = null

    override fun execute(action: AutomationAction): ActionResult {
        lastAction = action
        return executeResult
    }

    override fun findQueryMatch(selector: Selector): AgentQueryMatch? = null
}

private fun fakeTarget(
    text: String? = null,
    resourceId: String? = null,
    semanticKey: String? = null,
): UiTarget = UiTarget(
    targetId = "t1",
    role = null,
    uiRole = "text",
    label = null,
    semanticKey = semanticKey,
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

private fun fakeSnapshot(targets: List<UiTarget>): UiSnapshot = UiSnapshot(
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
