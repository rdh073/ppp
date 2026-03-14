package com.autosdk.agent.agent

import com.autosdk.agent.action.ActionExecutor
import com.autosdk.agent.action.ActionResult
import com.autosdk.agent.action.AutomationAction
import com.autosdk.agent.action.Selector

data class AgentQueryMatch(
    val resourceId: String?,
    val text: String?,
)

interface AgentAutomationDriver {
    fun execute(action: AutomationAction): ActionResult

    fun findQueryMatch(selector: Selector): AgentQueryMatch?
}

class AccessibilityAgentAutomationDriver(
    private val actionExecutor: ActionExecutor,
) : AgentAutomationDriver {
    override fun execute(action: AutomationAction): ActionResult =
        actionExecutor.execute(action)

    override fun findQueryMatch(selector: Selector): AgentQueryMatch? =
        actionExecutor.findNode(selector)?.let { node ->
            try {
                AgentQueryMatch(
                    resourceId = node.viewIdResourceName,
                    text = node.text?.toString(),
                )
            } finally {
                node.recycle()
            }
        }
}
