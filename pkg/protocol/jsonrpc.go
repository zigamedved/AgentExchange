package protocol

import "encoding/json"

// ─── JSON-RPC 2.0 ────────────────────────────────────────────────────────────

// Request is a JSON-RPC 2.0 request object.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// Response is a JSON-RPC 2.0 response object.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC + A2A error codes.
const (
	CodeParseError            = -32700
	CodeInvalidRequest        = -32600
	CodeMethodNotFound        = -32601
	CodeInvalidParams         = -32602
	CodeInternalError         = -32603
	CodeTaskNotFound          = -32001
	CodeContentTypeNotSupported = -32002
	CodeUnsupportedOperation  = -32003
	// FAXP-specific
	CodeAuthRequired           = -32010
	CodeForbidden              = -32011
	CodeQuoteExpired           = -32020
	CodeQuoteNotFound          = -32021
	CodePriceConstraintViolated = -32022
)

// NewRequest constructs a JSON-RPC 2.0 request.
func NewRequest(id, method string, params any) (*Request, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	rawID, err := json.Marshal(id)
	if err != nil {
		return nil, err
	}
	return &Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  raw,
		ID:      rawID,
	}, nil
}

// NewSuccessResponse constructs a successful JSON-RPC 2.0 response.
func NewSuccessResponse(id json.RawMessage, result any) (*Response, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &Response{
		JSONRPC: "2.0",
		Result:  raw,
		ID:      id,
	}, nil
}

// NewErrorResponse constructs an error JSON-RPC 2.0 response.
func NewErrorResponse(id json.RawMessage, code int, message string, data any) *Response {
	return &Response{
		JSONRPC: "2.0",
		Error:   &RPCError{Code: code, Message: message, Data: data},
		ID:      id,
	}
}

// ParseParams unmarshals the raw params into the target struct.
func (r *Request) ParseParams(target any) error {
	if r.Params == nil {
		return nil
	}
	return json.Unmarshal(r.Params, target)
}

// ParseResult unmarshals the raw result into the target struct.
func (r *Response) ParseResult(target any) error {
	if r.Result == nil {
		return nil
	}
	return json.Unmarshal(r.Result, target)
}
