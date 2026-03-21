package com.autosdk.agent.transport

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull

@Serializable
data class JsonRpcRequest(
    val jsonrpc: String = "2.0",
    val id: String,
    val method: String,
    val params: JsonElement = JsonNull,
)

@Serializable
data class JsonRpcNotification(
    val jsonrpc: String = "2.0",
    val method: String,
    val params: JsonElement = JsonNull,
)

@Serializable
data class JsonRpcSuccess(
    val jsonrpc: String = "2.0",
    val id: String,
    val result: JsonElement,
)

@Serializable
data class JsonRpcErrorObject(
    val code: Int,
    val message: String,
    val data: JsonElement? = null,
)

@Serializable
data class JsonRpcFailure(
    val jsonrpc: String = "2.0",
    val id: String,
    val error: JsonRpcErrorObject,
)

/** Standard JSON-RPC 2.0 and application-level error codes. */
object JsonRpcErrorCode {
    const val PARSE_ERROR = -32700
    const val INVALID_REQUEST = -32600
    const val METHOD_NOT_FOUND = -32601
    const val INVALID_PARAMS = -32602
    const val INTERNAL_ERROR = -32603

    // Application-level errors (-32000 to -32099)
    const val TARGET_NOT_FOUND = -32001
    const val PERMISSION_UNAVAILABLE = -32002
    const val DEVICE_UNAVAILABLE = -32003
    const val CAPABILITY_UNAVAILABLE = -32004
    const val TRANSITION_TIMEOUT = -32005
    const val TARGET_NOT_ACTIONABLE = -32006
    const val INPUT_REJECTED = -32007
    const val SCRIPT_ERROR = -32008
}
