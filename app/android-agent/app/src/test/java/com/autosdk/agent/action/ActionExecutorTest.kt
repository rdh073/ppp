package com.autosdk.agent.action

import android.accessibilityservice.AccessibilityService
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityNodeInfo
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf

@RunWith(RobolectricTestRunner::class)
class ActionExecutorTest {
    @Test
    fun `click resolves clickable ancestor for selected label node`() {
        val fixture = ExecutorFixture()
        val row = fixture.node(clickable = true)
        val label = fixture.node(text = "Private DNS")
        fixture.attach(fixture.root, row)
        fixture.attach(row, label)

        val performedActions = mutableListOf<Int>()
        shadowOf(row).setOnPerformActionListener { action, _ ->
            performedActions += action
            true
        }

        val result =
            fixture.executor.execute(
                AutomationAction.Click(Selector(SelectorKind.TARGET_ID, "private_dns_2")),
            )

        assertEquals(ActionResult.Ok, result)
        assertEquals(listOf(AccessibilityNodeInfo.ACTION_CLICK), performedActions)
        assertTrue(shadowOf(label).performedActions.isEmpty())
    }

    @Test
    fun `click fails when no clickable ancestor exists for selected label node`() {
        val fixture = ExecutorFixture()
        val row = fixture.node()
        val label = fixture.node(text = "Private DNS")
        fixture.attach(fixture.root, row)
        fixture.attach(row, label)

        val result =
            fixture.executor.execute(
                AutomationAction.Click(Selector(SelectorKind.TARGET_ID, "private_dns_2")),
            )

        assertEquals(
            ActionResult.Failed(
                "target_not_actionable",
                "Node and ancestors are not clickable: Selector(kind=TARGET_ID, value=private_dns_2)",
            ),
            result,
        )
    }

    @Test
    fun `click fails when clickable ancestor is disabled`() {
        val fixture = ExecutorFixture()
        val row = fixture.node(clickable = true, enabled = false)
        val label = fixture.node(text = "Private DNS")
        fixture.attach(fixture.root, row)
        fixture.attach(row, label)

        val result =
            fixture.executor.execute(
                AutomationAction.Click(Selector(SelectorKind.TARGET_ID, "private_dns_2")),
            )

        assertEquals(
            ActionResult.Failed(
                "target_not_actionable",
                "Node is disabled: Selector(kind=TARGET_ID, value=private_dns_2)",
            ),
            result,
        )
    }

    @Test
    fun `long press resolves long clickable ancestor for selected label node`() {
        val fixture = ExecutorFixture()
        val row = fixture.node(longClickable = true)
        val label = fixture.node(text = "Private DNS")
        fixture.attach(fixture.root, row)
        fixture.attach(row, label)

        val performedActions = mutableListOf<Int>()
        shadowOf(row).setOnPerformActionListener { action, _ ->
            performedActions += action
            true
        }

        val result =
            fixture.executor.execute(
                AutomationAction.LongPress(Selector(SelectorKind.TARGET_ID, "private_dns_2")),
            )

        assertEquals(ActionResult.Ok, result)
        assertEquals(listOf(AccessibilityNodeInfo.ACTION_LONG_CLICK), performedActions)
    }
}

private class ExecutorFixture {
    val service = Robolectric.setupService(TestAccessibilityService::class.java)
    val root: AccessibilityNodeInfo = node()
    val executor = ActionExecutor(service)

    init {
        shadowOf(service).setRootInActiveWindow(root)
    }

    fun node(
        text: String? = null,
        clickable: Boolean = false,
        longClickable: Boolean = false,
        enabled: Boolean = true,
    ): AccessibilityNodeInfo =
        AccessibilityNodeInfo.obtain().apply {
            if (text != null) {
                setText(text)
            }
            setClickable(clickable)
            setLongClickable(longClickable)
            setEnabled(enabled)
            setVisibleToUser(true)
        }

    fun attach(
        parent: AccessibilityNodeInfo,
        child: AccessibilityNodeInfo,
    ) {
        shadowOf(parent).addChild(child)
    }
}

private class TestAccessibilityService : AccessibilityService() {
    override fun onAccessibilityEvent(event: AccessibilityEvent?) = Unit

    override fun onInterrupt() = Unit
}
