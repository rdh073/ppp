package com.autosdk.agent.agent

import com.autosdk.agent.action.ActionResult
import com.autosdk.agent.action.AutomationAction
import com.autosdk.agent.action.Selector
import com.autosdk.agent.transport.AgentTransport
import com.autosdk.agent.transport.JsonRpcErrorCode
import com.autosdk.agent.transport.JsonRpcRequest
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import okhttp3.OkHttpClient
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Integration tests for the [AgentRuntime] `device.script` handler.
 */
class AgentRuntimeScriptTest {

    private fun makeRuntime(
        executeResult: ActionResult = ActionResult.Ok,
    ): Pair<AgentRuntime, ScriptFakeTransport> {
        val transport = ScriptFakeTransport()
        val driver = ScriptFakeDriver(executeResult)
        val runtime = AgentRuntime(
            transport = transport,
            snapshotBuilder = { null },
            settle = {},
            automationDriver = driver,
            deviceId = "test-device",
            capabilities = emptyList(),
            okHttpClient = OkHttpClient(),
        )
        runtime.start()
        return runtime to transport
    }

    private fun scriptReq(
        id: String = "req-1",
        source: String,
        params: Map<String, String> = emptyMap(),
        timeoutMs: Long = 5_000L,
    ): JsonRpcRequest {
        val p = buildJsonObject {
            put("script", source)
            put("timeout", timeoutMs)
            if (params.isNotEmpty()) {
                put("params", buildJsonObject { params.forEach { (k, v) -> put(k, v) } })
            }
        }
        return JsonRpcRequest(id = id, method = "device.script", params = p)
    }

    @Test
    fun `device_script returns output object on success`() = runBlocking {
        val (_, transport) = makeRuntime()
        transport.emitRequest(scriptReq(source = "return { done: true };"))
        assertEquals(1, transport.successes.size)
        assertEquals(0, transport.errors.size)
        val output = transport.successes[0].result.jsonObject["output"]?.jsonObject
        assertNotNull(output)
        assertEquals("true", output!!["done"]?.jsonPrimitive?.content)
    }

    @Test
    fun `device_script injects params into script scope`() = runBlocking {
        val (_, transport) = makeRuntime()
        transport.emitRequest(scriptReq(
            source = "return { host: params.hostname };",
            params = mapOf("hostname" to "dns.example.com"),
        ))
        assertEquals(1, transport.successes.size)
        val output = transport.successes[0].result.jsonObject["output"]?.jsonObject
        assertEquals("dns.example.com", output?.get("host")?.jsonPrimitive?.content)
    }

    @Test
    fun `device_script thrown error returns SCRIPT_ERROR`() = runBlocking {
        val (_, transport) = makeRuntime()
        transport.emitRequest(scriptReq(source = "throw new Error('boom');"))
        assertEquals(0, transport.successes.size)
        assertEquals(1, transport.errors.size)
        assertEquals(JsonRpcErrorCode.SCRIPT_ERROR, transport.errors[0].code)
        assertTrue(transport.errors[0].message.contains("boom"))
    }

    @Test
    fun `device_script missing script field returns INVALID_PARAMS`() = runBlocking {
        val (_, transport) = makeRuntime()
        val badReq = JsonRpcRequest(
            id = "req-bad",
            method = "device.script",
            params = buildJsonObject { },
        )
        transport.emitRequest(badReq)
        assertEquals(1, transport.errors.size)
        assertEquals(JsonRpcErrorCode.INVALID_PARAMS, transport.errors[0].code)
    }

    @Test
    fun `device_script includes logs array in response`() = runBlocking {
        val (_, transport) = makeRuntime()
        transport.emitRequest(scriptReq(source = "log('a'); log('b'); return {};"))
        assertEquals(1, transport.successes.size)
        val logs = transport.successes[0].result.jsonObject["logs"]?.jsonArray
            ?.map { it.jsonPrimitive.content }
        assertEquals(listOf("a", "b"), logs)
    }

    @Test
    fun `device_script includes durationMs in response`() = runBlocking {
        val (_, transport) = makeRuntime()
        transport.emitRequest(scriptReq(source = "return {};"))
        assertEquals(1, transport.successes.size)
        val duration = transport.successes[0].result.jsonObject["durationMs"]?.jsonPrimitive?.content?.toLongOrNull()
        assertNotNull(duration)
        assertTrue(duration!! >= 0)
    }

    @Test
    fun `device_script serializes array output correctly`() = runBlocking {
        val (_, transport) = makeRuntime()
        transport.emitRequest(scriptReq(source = "return { items: [1, 'x', true] };"))
        assertEquals(1, transport.successes.size)
        val items = transport.successes[0].result.jsonObject["output"]?.jsonObject?.get("items")?.jsonArray
        assertNotNull(items)
        assertEquals(3, items!!.size)
        assertEquals("1.0", items[0].jsonPrimitive.content)
        assertEquals("x", items[1].jsonPrimitive.content)
        assertEquals("true", items[2].jsonPrimitive.content)
    }
}

// ---- test doubles ----

private class ScriptFakeTransport : AgentTransport {
    data class SuccessPayload(val id: String, val result: JsonElement)
    data class ErrorPayload(val id: String, val code: Int, val message: String)

    val successes = mutableListOf<SuccessPayload>()
    val errors = mutableListOf<ErrorPayload>()

    private var requestHandler: (suspend (JsonRpcRequest) -> Unit)? = null

    override suspend fun connect() = Unit
    override fun disconnect() = Unit
    override suspend fun sendSuccess(id: String, result: JsonElement) { successes += SuccessPayload(id, result) }
    override suspend fun sendError(id: String, code: Int, message: String) { errors += ErrorPayload(id, code, message) }
    override fun onRequest(handler: suspend (JsonRpcRequest) -> Unit) { requestHandler = handler }
    override fun onConnected(handler: () -> Unit) = Unit
    override fun onDisconnected(handler: () -> Unit) = Unit

    suspend fun emitRequest(req: JsonRpcRequest) { requestHandler?.invoke(req) }
}

private class ScriptFakeDriver(
    private val executeResult: ActionResult = ActionResult.Ok,
) : AgentAutomationDriver {
    override fun execute(action: AutomationAction): ActionResult = executeResult
    override fun findQueryMatch(selector: Selector): AgentQueryMatch? = null
}
