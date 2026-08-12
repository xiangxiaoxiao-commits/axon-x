package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
)

// This file implements the slice of the Model Context Protocol (MCP) that a
// stdio server needs: JSON-RPC 2.0 framed as newline-delimited JSON over
// stdin/stdout, plus the initialize / tools.list / tools.call methods. It is
// hand-rolled on the standard library to avoid pulling a heavy dependency into
// the build; the wire shapes follow the MCP spec (2024-11-05).

// protocolVersion is the MCP revision this server implements. Claude Code sends
// its own version in initialize; we echo a version we support.
const protocolVersion = "2024-11-05"

// rpcRequest is an incoming JSON-RPC 2.0 request or notification. ID is absent
// (null) for notifications, which must not be answered.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is an outgoing JSON-RPC 2.0 response. Exactly one of Result/Error
// is set.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC standard error codes used here.
const (
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// server wires the transport to the tool handlers.
type server struct {
	w    *bufio.Writer
	tool *toolHandler
}

// serve reads requests line-by-line and dispatches them until stdin closes.
// Newline-delimited JSON keeps the transport dependency-free; each response is
// written on its own line.
func (s *server) serve(r io.Reader) error {
	sc := bufio.NewScanner(r)
	// Sessions can carry large tool payloads; raise the line cap well above the
	// 64KB default so a big request line is never silently truncated.
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			log.Printf("axon-mcp: bad request json: %v", err)
			continue
		}
		s.dispatch(req)
	}
	return sc.Err()
}

// dispatch routes one request to its handler. Notifications (no id) are executed
// for side effects but never answered, per JSON-RPC.
func (s *server) dispatch(req rpcRequest) {
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	switch req.Method {
	case "initialize":
		s.reply(req.ID, s.handleInitialize())
	case "notifications/initialized", "initialized":
		// Client handshake ack; nothing to do.
	case "ping":
		s.reply(req.ID, map[string]interface{}{})
	case "tools/list":
		s.reply(req.ID, s.tool.list())
	case "tools/call":
		result, err := s.tool.call(req.Params)
		if err != nil {
			s.replyError(req.ID, codeInvalidParams, err.Error())
			return
		}
		s.reply(req.ID, result)
	default:
		if !isNotification {
			s.replyError(req.ID, codeMethodNotFound, fmt.Sprintf("method %q not found", req.Method))
		}
	}
}

// handleInitialize returns the server capabilities and identity.
func (s *server) handleInitialize() map[string]interface{} {
	return map[string]interface{}{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "axon-knowledge",
			"version": "0.1.0",
		},
	}
}

// reply writes a success response for id. Notifications (empty id) get nothing.
func (s *server) reply(id json.RawMessage, result interface{}) {
	if len(id) == 0 || string(id) == "null" {
		return
	}
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

// replyError writes an error response for id.
func (s *server) replyError(id json.RawMessage, code int, msg string) {
	if len(id) == 0 || string(id) == "null" {
		return
	}
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

// write marshals and flushes one response as a single JSON line.
func (s *server) write(resp rpcResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("axon-mcp: marshal response: %v", err)
		return
	}
	s.w.Write(data)
	s.w.WriteByte('\n')
	s.w.Flush()
}
