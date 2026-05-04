// Package mcp implements a minimal MCP (Model Context Protocol) stdio server
// using JSON-RPC 2.0 with newline-delimited framing.
//
// Sized to host Saga's tool surface; not a generic SDK. We implement directly
// rather than depending on an external MCP client library because the protocol
// surface we need is small (initialize, tools/list, tools/call, ping) and
// stable, and a hand-rolled implementation avoids version drift in the
// ecosystem during the v2 rewrite.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

const protocolVersion = "2025-03-26"

// Tool descriptor sent in tools/list responses.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Result is the success payload returned by a tool handler.
// IsError signals a tool-level error that the LLM should see (vs a
// protocol-level error which is reported via JSON-RPC error envelope).
type Result struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// TextResult is a convenience constructor for a single text content block.
func TextResult(text string) Result {
	return Result{Content: []Content{{Type: "text", Text: text}}}
}

// ErrorResult signals a tool-level error (still 2xx at protocol level,
// but isError=true so the LLM treats the body as an error message).
func ErrorResult(text string) Result {
	return Result{Content: []Content{{Type: "text", Text: text}}, IsError: true}
}

// Handler dispatches a tools/call invocation.
//   - Return a Result (with or without IsError) for normal tool outcomes.
//   - Return a non-nil error only for protocol-level failures
//     (unknown tool name, infrastructure errors). The server converts
//     non-nil errors into JSON-RPC error envelopes (-32603).
type Handler func(ctx context.Context, name string, args json.RawMessage) (Result, error)

type Server struct {
	name    string
	version string
	tools   []Tool
	handler Handler

	mu  sync.Mutex
	out io.Writer
}

func New(name, version string, tools []Tool, handler Handler) *Server {
	return &Server{name: name, version: version, tools: tools, handler: handler}
}

// JSON-RPC 2.0 envelopes.
type rawRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rawResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Serve reads JSON-RPC messages line-by-line from in and writes responses to
// out. Returns when in returns EOF or scanner hits an error.
//
// Each line is one complete JSON-RPC message (newline-delimited framing).
// Malformed lines produce a parse-error response and processing continues.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	s.out = out
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rawRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeError(nil, -32700, "parse error: "+err.Error())
			continue
		}
		s.dispatch(ctx, req)
	}
	return scanner.Err()
}

func (s *Server) dispatch(ctx context.Context, req rawRequest) {
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	switch req.Method {
	case "initialize":
		s.handleInitialize(req.ID)
	case "notifications/initialized", "notifications/cancelled":
		// Notifications expect no response.
	case "ping":
		s.writeResult(req.ID, struct{}{})
	case "tools/list":
		s.writeResult(req.ID, map[string]any{"tools": s.tools})
	case "tools/call":
		s.handleToolsCall(ctx, req.ID, req.Params)
	default:
		if !isNotification {
			s.writeError(req.ID, -32601, "method not found: "+req.Method)
		}
	}
}

func (s *Server) handleInitialize(id json.RawMessage) {
	s.writeResult(id, map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    s.name,
			"version": s.version,
		},
	})
}

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) handleToolsCall(ctx context.Context, id json.RawMessage, params json.RawMessage) {
	var p toolsCallParams
	if err := json.Unmarshal(params, &p); err != nil {
		s.writeError(id, -32602, "invalid params: "+err.Error())
		return
	}
	res, err := s.handler(ctx, p.Name, p.Arguments)
	if err != nil {
		s.writeError(id, -32603, err.Error())
		return
	}
	s.writeResult(id, res)
}

func (s *Server) writeResult(id json.RawMessage, result any) {
	s.writeFrame(rawResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) writeError(id json.RawMessage, code int, msg string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	s.writeFrame(rawResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func (s *Server) writeFrame(resp rawResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bytes, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(s.out, `{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"marshal error"}}`+"\n")
		return
	}
	_, _ = s.out.Write(bytes)
	_, _ = s.out.Write([]byte{'\n'})
}
