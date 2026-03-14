package com.autosdk.agent.transport

import kotlinx.serialization.json.JsonElement

interface AgentTransport {
    /** Connect to the control server and block until the connection is established. */
    suspend fun connect()

    /** Disconnect from the control server. */
    fun disconnect()

    /** Send a JSON-RPC success response for the given request id. */
    suspend fun sendSuccess(id: String, result: JsonElement)

    /** Send a JSON-RPC error response for the given request id. */
    suspend fun sendError(id: String, code: Int, message: String)

    /** Register the handler that receives incoming JSON-RPC requests from the server. */
    fun onRequest(handler: suspend (JsonRpcRequest) -> Unit)

    /** Called when the transport connects (or reconnects) successfully. */
    fun onConnected(handler: () -> Unit)

    /** Called when the transport loses its connection. */
    fun onDisconnected(handler: () -> Unit)
}
