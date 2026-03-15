package com.autosdk.agent.transport

import com.autosdk.agent.state.AgentTransportDriver
import android.util.Log
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import java.net.Proxy
import java.util.concurrent.TimeUnit
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

// One shared client across all transport instances — thread pools and connection pools
// are expensive and must not be recreated on every reconnect.
private val sharedClient: OkHttpClient by lazy {
    createSharedClient()
}

internal fun createSharedClient(): OkHttpClient =
    OkHttpClient.Builder()
        .proxy(Proxy.NO_PROXY)
        .connectTimeout(10, TimeUnit.SECONDS)
        .build()

private const val TAG = "WebSocketTransport"

/**
 * WebSocket-based JSON-RPC 2.0 transport.
 *
 * The transport owns framing, socket lifecycle, and JSON-RPC encode/decode.
 * It does not own handshake policy; `agent.hello`, `agent.resume`,
 * `agent.heartbeat`, and `agent.disconnect` are sent by the coordinator.
 */
class WebSocketAgentTransport(
    private val serverUrl: String,
    private val scope: CoroutineScope,
) : AgentTransport, AgentTransportDriver {

    private val client = sharedClient

    private val json =
        Json {
            ignoreUnknownKeys = true
            encodeDefaults = true
        }

    @Volatile private var webSocket: WebSocket? = null

    private var requestHandler: (suspend (JsonRpcRequest) -> Unit)? = null
    private var connectedHandler: (() -> Unit)? = null
    private var disconnectedHandler: (() -> Unit)? = null
    private var socketOpenedHandler: (() -> Unit)? = null
    private var socketClosedHandler: ((String) -> Unit)? = null
    private var socketFailureHandler: ((String) -> Unit)? = null
    private var rpcSuccessHandler: ((String, JsonElement) -> Unit)? = null
    private var rpcFailureHandler: ((String, Int, String) -> Unit)? = null

    override suspend fun connect() {
        val request = Request.Builder().url(serverUrl).build()

        return suspendCancellableCoroutine { cont ->
            val listener = object : WebSocketListener() {
                override fun onOpen(ws: WebSocket, response: Response) {
                    handleSocketOpened(ws)

                    if (cont.isActive) cont.resume(Unit)
                }

                override fun onMessage(ws: WebSocket, text: String) {
                    scope.launch {
                        try {
                            dispatchIncomingMessage(text)
                        } catch (e: Exception) {
                            logError("Failed to parse incoming message: $text", e)
                        }
                    }
                }

                override fun onFailure(ws: WebSocket, t: Throwable, response: Response?) {
                    val reason = handleSocketFailure(t)
                    if (cont.isActive) {
                        // Initial connect failed — resume with exception so the caller
                        // handles the retry. Do NOT invoke transport drop handlers here;
                        // that handler is for drops after a successful connection.
                        cont.resumeWithException(CancellationException("WebSocket failure: ${t.message}", t))
                    } else {
                        // Connection was established and then dropped.
                        notifySocketFailure(reason)
                    }
                }

                override fun onClosed(ws: WebSocket, code: Int, reason: String) {
                    handleSocketClosed(code, reason)
                }
            }

            val ws = client.newWebSocket(request, listener)
            cont.invokeOnCancellation { ws.cancel() }
        }
    }

    override fun disconnect() {
        webSocket?.close(1000, "Agent shutting down")
        webSocket = null
    }

    override suspend fun sendRequest(
        id: String,
        method: String,
        params: JsonElement,
    ) {
        val socket = webSocket ?: error("No active WebSocket for method=$method")
        val request = JsonRpcRequest(id = id, method = method, params = params)
        socket.send(json.encodeToString(request))
    }

    override suspend fun sendNotification(
        method: String,
        params: JsonElement,
    ): Boolean {
        val socket = webSocket ?: run {
            logWarn("sendNotification: no active WebSocket for method=$method")
            return false
        }
        val notification = JsonRpcNotification(method = method, params = params)
        return socket.send(json.encodeToString(notification))
    }

    override suspend fun sendSuccess(id: String, result: JsonElement) {
        val response = JsonRpcSuccess(id = id, result = result)
        webSocket?.send(json.encodeToString(response))
            ?: logWarn("sendSuccess: no active WebSocket for id=$id")
    }

    override suspend fun sendError(id: String, code: Int, message: String) {
        val response = JsonRpcFailure(id = id, error = JsonRpcErrorObject(code = code, message = message))
        webSocket?.send(json.encodeToString(response))
            ?: logWarn("sendError: no active WebSocket for id=$id")
    }

    override fun onRequest(handler: suspend (JsonRpcRequest) -> Unit) {
        requestHandler = handler
    }

    override fun onConnected(handler: () -> Unit) {
        connectedHandler = handler
    }

    override fun onDisconnected(handler: () -> Unit) {
        disconnectedHandler = handler
    }

    override fun onSocketOpened(handler: () -> Unit) {
        socketOpenedHandler = handler
    }

    override fun onSocketClosed(handler: (String) -> Unit) {
        socketClosedHandler = handler
    }

    override fun onSocketFailure(handler: (String) -> Unit) {
        socketFailureHandler = handler
    }

    override fun onRpcSuccess(handler: (String, JsonElement) -> Unit) {
        rpcSuccessHandler = handler
    }

    override fun onRpcFailure(handler: (String, Int, String) -> Unit) {
        rpcFailureHandler = handler
    }

    internal fun handleSocketOpened(ws: WebSocket) {
        webSocket = ws
        logInfo("Connected to $serverUrl")
        socketOpenedHandler?.invoke()
        connectedHandler?.invoke()
    }

    internal fun handleSocketClosed(
        code: Int,
        reason: String,
    ) {
        logInfo("WebSocket closed: $code $reason")
        webSocket = null
        socketClosedHandler?.invoke(reason.ifBlank { "closed:$code" })
        disconnectedHandler?.invoke()
    }

    internal fun handleSocketFailure(t: Throwable): String {
        logError("WebSocket failure", t)
        webSocket = null
        return t.message ?: "websocket_failure"
    }

    internal fun notifySocketFailure(reason: String) {
        socketFailureHandler?.invoke(reason)
        disconnectedHandler?.invoke()
    }

    internal suspend fun dispatchIncomingMessage(text: String) {
        val payload = json.parseToJsonElement(text).jsonObject
        when {
            payload.containsKey("method") -> {
                val request = json.decodeFromJsonObject<JsonRpcRequest>(payload)
                requestHandler?.invoke(request)
            }

            payload.containsKey("result") -> {
                val success = json.decodeFromJsonObject<JsonRpcSuccess>(payload)
                rpcSuccessHandler?.invoke(success.id, success.result)
            }

            payload.containsKey("error") -> {
                val failure = json.decodeFromJsonObject<JsonRpcFailure>(payload)
                rpcFailureHandler?.invoke(failure.id, failure.error.code, failure.error.message)
            }

            else -> logWarn("Ignoring unsupported JSON-RPC payload: $text")
        }
    }
}

private inline fun <reified T> Json.decodeFromJsonObject(payload: JsonObject): T =
    decodeFromString(payload.toString())

private fun logInfo(message: String) {
    runCatching { Log.i(TAG, message) }
}

private fun logWarn(message: String) {
    runCatching { Log.w(TAG, message) }
}

private fun logError(
    message: String,
    error: Throwable,
) {
    runCatching { Log.e(TAG, message, error) }
}
