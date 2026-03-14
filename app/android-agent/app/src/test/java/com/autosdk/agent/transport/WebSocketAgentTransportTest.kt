package com.autosdk.agent.transport

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import okhttp3.Request
import okhttp3.WebSocket
import okio.ByteString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.net.Proxy

class WebSocketAgentTransportTest {
    @Test
    fun `shared client bypasses device global proxy`() {
        val client = createSharedClient()

        assertEquals(Proxy.NO_PROXY, client.proxy)
        assertEquals(0, client.pingIntervalMillis)
    }

    @Test
    fun `socket open does not auto register`() {
        val transport = createTransport()
        val socket = FakeWebSocket()
        var opened = false

        transport.onSocketOpened { opened = true }
        transport.handleSocketOpened(socket)

        assertTrue(opened)
        assertTrue(socket.sentMessages.isEmpty())
    }

    @Test
    fun `incoming request is forwarded to runtime handler`() =
        runBlocking {
            val transport = createTransport()
            val received = mutableListOf<JsonRpcRequest>()

            transport.onRequest { request -> received += request }
            transport.dispatchIncomingMessage(
                """
                {"jsonrpc":"2.0","id":"req-1","method":"device.observe","params":{"deviceId":"device-1"}}
                """.trimIndent(),
            )

            assertEquals(1, received.size)
            assertEquals("device.observe", received.single().method)
            assertEquals("req-1", received.single().id)
        }

    @Test
    fun `incoming success response is forwarded to rpc success handler`() =
        runBlocking {
            val transport = createTransport()
            val results = mutableListOf<Pair<String, String>>()

            transport.onRpcSuccess { id, result ->
                results += id to result.toString()
            }
            transport.dispatchIncomingMessage(
                """
                {"jsonrpc":"2.0","id":"hello-1","result":{"accepted":true,"sessionId":"session-1"}}
                """.trimIndent(),
            )

            assertEquals(listOf("hello-1" to """{"accepted":true,"sessionId":"session-1"}"""), results)
        }

    @Test
    fun `incoming failure response is forwarded to rpc failure handler`() =
        runBlocking {
            val transport = createTransport()
            val failures = mutableListOf<Triple<String, Int, String>>()

            transport.onRpcFailure { id, code, message ->
                failures += Triple(id, code, message)
            }
            transport.dispatchIncomingMessage(
                """
                {"jsonrpc":"2.0","id":"resume-1","error":{"code":-32003,"message":"stale session"}}
                """.trimIndent(),
            )

            assertEquals(listOf(Triple("resume-1", -32003, "stale session")), failures)
        }

    @Test
    fun `send request writes encoded json to active socket`() =
        runBlocking {
            val transport = createTransport()
            val socket = FakeWebSocket()
            transport.handleSocketOpened(socket)

            transport.sendRequest(
                id = "hello-1",
                method = "agent.hello",
                params =
                    buildJsonObject {
                        put("deviceId", "device-1")
                        put("agentInstanceId", "agent-1")
                    },
            )

            assertEquals(1, socket.sentMessages.size)
            assertTrue(socket.sentMessages.single().contains(""""jsonrpc":"2.0""""))
            assertTrue(socket.sentMessages.single().contains(""""method":"agent.hello""""))
            assertTrue(socket.sentMessages.single().contains(""""deviceId":"device-1""""))
        }

    @Test
    fun `send notification returns false when socket is unavailable`() =
        runBlocking {
            val transport = createTransport()
            val sent =
                transport.sendNotification(
                    method = "android.accessibility.disabled",
                    params =
                        buildJsonObject {
                            put("seqNo", 1)
                        },
                )

            assertFalse(sent)
        }

    @Test
    fun `send notification writes encoded json to active socket`() =
        runBlocking {
            val transport = createTransport()
            val socket = FakeWebSocket()
            transport.handleSocketOpened(socket)

            val sent =
                transport.sendNotification(
                    method = "android.accessibility.disabled",
                    params =
                        buildJsonObject {
                            put("seqNo", 5)
                            put("reason", "service_interrupted")
                        },
                )

            assertTrue(sent)
            assertEquals(1, socket.sentMessages.size)
            assertTrue(socket.sentMessages.single().contains(""""method":"android.accessibility.disabled""""))
            assertTrue(socket.sentMessages.single().contains(""""seqNo":5"""))
        }

    @Test
    fun `socket failure notifies failure and disconnected handlers`() {
        val transport = createTransport()
        val reasons = mutableListOf<String>()
        var disconnected = 0

        transport.onSocketFailure { reason -> reasons += reason }
        transport.onDisconnected { disconnected += 1 }

        val reason = transport.handleSocketFailure(IllegalStateException("network dropped"))
        transport.notifySocketFailure(reason)

        assertEquals(listOf("network dropped"), reasons)
        assertEquals(1, disconnected)
    }

    @Test
    fun `disconnect closes active socket`() {
        val transport = createTransport()
        val socket = FakeWebSocket()
        transport.handleSocketOpened(socket)

        transport.disconnect()

        assertTrue(socket.closed)
        assertEquals(1000, socket.closeCode)
        assertEquals("Agent shutting down", socket.closeReason)
    }

    @Test
    fun `closed socket reason falls back to code when reason blank`() {
        val transport = createTransport()
        val reasons = mutableListOf<String>()
        var disconnected = 0

        transport.onSocketClosed { reason -> reasons += reason }
        transport.onDisconnected { disconnected += 1 }

        transport.handleSocketClosed(1006, "")

        assertEquals(listOf("closed:1006"), reasons)
        assertEquals(1, disconnected)
    }

    private fun createTransport(): WebSocketAgentTransport =
        WebSocketAgentTransport(
            serverUrl = "ws://example.invalid/ws/agent",
            scope = CoroutineScope(Dispatchers.Unconfined + Job()),
        )
}

private class FakeWebSocket : WebSocket {
    val sentMessages = mutableListOf<String>()
    var closed = false
    var closeCode: Int? = null
    var closeReason: String? = null

    override fun request(): Request =
        Request.Builder().url("ws://example.invalid/ws/agent").build()

    override fun queueSize(): Long = 0L

    override fun send(text: String): Boolean {
        sentMessages += text
        return true
    }

    override fun send(bytes: ByteString): Boolean = false

    override fun close(
        code: Int,
        reason: String?,
    ): Boolean {
        closed = true
        closeCode = code
        closeReason = reason
        return true
    }

    override fun cancel() {
        closed = true
    }
}
