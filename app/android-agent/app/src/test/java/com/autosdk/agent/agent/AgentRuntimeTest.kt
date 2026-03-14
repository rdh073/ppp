package com.autosdk.agent.agent

import com.autosdk.agent.action.ActionResult
import com.autosdk.agent.action.AutomationAction
import com.autosdk.agent.action.Selector
import com.autosdk.agent.observation.UiSnapshot
import com.autosdk.agent.observation.UiTarget
import com.autosdk.agent.state.AgentEvent
import com.autosdk.agent.transport.AgentTransport
import com.autosdk.agent.transport.JsonRpcRequest
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Test

class AgentRuntimeTest {
    @Test
    fun `device execute emits execution events and returns success`() =
        runBlocking {
            val transport = FakeAgentTransport()
            val automationDriver = FakeAutomationDriver(actionResult = ActionResult.Ok)
            val snapshots =
                SnapshotSequence(
                    listOf(
                        snapshot(snapshotId = "before"),
                        snapshot(snapshotId = "after"),
                    ),
                )
            val events = mutableListOf<AgentEvent>()
            var settleCalls = 0

            val runtime =
                AgentRuntime(
                    transport = transport,
                    snapshotBuilder = { snapshots.next() },
                    settle = { settleCalls += 1 },
                    automationDriver = automationDriver,
                    deviceId = "device-1",
                    capabilities = emptyList(),
                    onExecutionEvent = { event -> events += event },
                )

            runtime.start()
            transport.emitRequest(
                JsonRpcRequest(
                    id = "req-1",
                    method = "device.execute",
                    params =
                        buildJsonObject {
                            put(
                                "action",
                                buildJsonObject {
                                    put("kind", "home")
                                },
                            )
                        },
                ),
            )

            assertEquals(1, settleCalls)
            assertEquals(1, transport.successes.size)
            assertEquals(0, transport.errors.size)
            assertEquals(
                listOf(
                    AgentEvent.ExecuteRequested("req-1"),
                    AgentEvent.SnapshotBeforeBuilt("req-1"),
                    AgentEvent.ActionSucceeded("req-1"),
                    AgentEvent.SettleCompleted("req-1", timedOut = false),
                    AgentEvent.SnapshotAfterBuilt("req-1"),
                    AgentEvent.ResponseSent("req-1"),
                ),
                events,
            )
        }

    @Test
    fun `device execute emits action failure and error response`() =
        runBlocking {
            val transport = FakeAgentTransport()
            val automationDriver =
                FakeAutomationDriver(
                    actionResult = ActionResult.Failed("input_rejected", "Action rejected"),
                )
            val snapshots = SnapshotSequence(listOf(snapshot(snapshotId = "before")))
            val events = mutableListOf<AgentEvent>()

            val runtime =
                AgentRuntime(
                    transport = transport,
                    snapshotBuilder = { snapshots.next() },
                    settle = {},
                    automationDriver = automationDriver,
                    deviceId = "device-1",
                    capabilities = emptyList(),
                    onExecutionEvent = { event -> events += event },
                )

            runtime.start()
            transport.emitRequest(
                JsonRpcRequest(
                    id = "req-2",
                    method = "device.execute",
                    params =
                        buildJsonObject {
                            put(
                                "action",
                                buildJsonObject {
                                    put("kind", "home")
                                },
                            )
                        },
                ),
            )

            assertEquals(0, transport.successes.size)
            assertEquals(1, transport.errors.size)
            assertEquals(
                listOf(
                    AgentEvent.ExecuteRequested("req-2"),
                    AgentEvent.SnapshotBeforeBuilt("req-2"),
                    AgentEvent.ActionFailed("req-2", "Action rejected"),
                    AgentEvent.ResponseSent("req-2"),
                ),
                events,
            )
        }

    @Test
    fun `device execute emits snapshot before failure when tree unavailable`() =
        runBlocking {
            val transport = FakeAgentTransport()
            val automationDriver = FakeAutomationDriver(actionResult = ActionResult.Ok)
            val events = mutableListOf<AgentEvent>()

            val runtime =
                AgentRuntime(
                    transport = transport,
                    snapshotBuilder = { null },
                    settle = {},
                    automationDriver = automationDriver,
                    deviceId = "device-1",
                    capabilities = emptyList(),
                    onExecutionEvent = { event -> events += event },
                )

            runtime.start()
            transport.emitRequest(
                JsonRpcRequest(
                    id = "req-3",
                    method = "device.execute",
                    params =
                        buildJsonObject {
                            put(
                                "action",
                                buildJsonObject {
                                    put("kind", "home")
                                },
                            )
                        },
                ),
            )

            assertEquals(0, transport.successes.size)
            assertEquals(1, transport.errors.size)
            assertEquals(
                listOf(
                    AgentEvent.ExecuteRequested("req-3"),
                    AgentEvent.SnapshotBeforeFailed("req-3", "Accessibility tree not available"),
                    AgentEvent.ResponseSent("req-3"),
                ),
                events,
            )
        }
}

private class FakeAgentTransport : AgentTransport {
    data class SuccessPayload(
        val id: String,
        val result: JsonElement,
    )

    data class ErrorPayload(
        val id: String,
        val code: Int,
        val message: String,
    )

    val successes = mutableListOf<SuccessPayload>()
    val errors = mutableListOf<ErrorPayload>()

    private var requestHandler: (suspend (JsonRpcRequest) -> Unit)? = null

    override suspend fun connect() = Unit

    override fun disconnect() = Unit

    override suspend fun sendSuccess(
        id: String,
        result: JsonElement,
    ) {
        successes += SuccessPayload(id, result)
    }

    override suspend fun sendError(
        id: String,
        code: Int,
        message: String,
    ) {
        errors += ErrorPayload(id, code, message)
    }

    override fun onRequest(handler: suspend (JsonRpcRequest) -> Unit) {
        requestHandler = handler
    }

    override fun onConnected(handler: () -> Unit) = Unit

    override fun onDisconnected(handler: () -> Unit) = Unit

    suspend fun emitRequest(request: JsonRpcRequest) {
        requestHandler?.invoke(request)
    }
}

private class FakeAutomationDriver(
    private val actionResult: ActionResult,
    private val queryMatch: AgentQueryMatch? = null,
) : AgentAutomationDriver {
    override fun execute(action: AutomationAction): ActionResult = actionResult

    override fun findQueryMatch(selector: Selector): AgentQueryMatch? = queryMatch
}

private class SnapshotSequence(
    private val snapshots: List<UiSnapshot?>,
) {
    private var index = 0

    fun next(): UiSnapshot? {
        if (index >= snapshots.size) {
            return snapshots.lastOrNull()
        }

        return snapshots[index++]
    }
}

private fun snapshot(snapshotId: String): UiSnapshot =
    UiSnapshot(
        snapshotId = snapshotId,
        deviceId = "device-1",
        packageName = "com.example.app",
        activityName = "MainActivity",
        screenState = null,
        capturedAt = "2026-03-12T00:00:00Z",
        targets =
            listOf(
                UiTarget(
                    targetId = "target-1",
                    role = "button",
                    text = "Open",
                    contentDesc = null,
                    resourceId = "button.open",
                    packageName = "com.example.app",
                    bounds = intArrayOf(0, 0, 10, 10),
                    actionable = true,
                    enabled = true,
                    checked = null,
                    selected = false,
                    scrollable = false,
                    focused = false,
                    password = false,
                ),
            ),
    )
