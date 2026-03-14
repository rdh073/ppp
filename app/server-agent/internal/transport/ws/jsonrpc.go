package ws

import "encoding/json"

// JSON-RPC 2.0 error codes, mirroring the android-agent's JsonRpcErrorCode.
const (
	ErrParseError    = -32700
	ErrInvalidReq    = -32600
	ErrMethodUnknown = -32601
	ErrInvalidParams = -32602
	ErrInternal      = -32603

	// Application-level (-32000 to -32099)
	ErrTargetNotFound      = -32001
	ErrPermissionDenied    = -32002
	ErrDeviceUnavailable   = -32003
	ErrCapabilityAbsent    = -32004
	ErrTransitionTimeout   = -32005
	ErrTargetNotActionable = -32006
	ErrInputRejected       = -32007
)

// rpcRequest is a JSON-RPC 2.0 request (server → agent or agent → server).
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC 2.0 response (either success or error).
type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      string    `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// inbound is used to parse an incoming WebSocket message before routing it.
// If Method is non-empty it is a request; otherwise it is a response.
type inbound struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
