// Package mcp adds tools from Model Context Protocol servers to Yggdrasil,
// and offers Yggdrasil's own AI to other apps as an MCP server.
//
// A server the person adds, from the gallery, a pasted snippet, another
// app's settings, or by hand, becomes a tool source: its tools join the
// same registry as the built-in and connected-service tools, with the same
// policies, selection, and credential handling (spec §32). Servers that run
// on this computer start when a tool is first needed and stop when idle, as
// models do.
package mcp

import (
	"encoding/json"
	"fmt"
)

// ProtocolVersion is the version this client asks for. A server may answer
// with any version in supportedVersions.
const ProtocolVersion = "2025-06-18"

var supportedVersions = map[string]bool{
	"2025-11-25": true,
	"2025-06-18": true,
	"2025-03-26": true,
	"2024-11-05": true,
}

// message is one JSON-RPC 2.0 message: a request (ID and Method), a
// notification (Method only), or a response (ID with Result or Error).
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func (m message) isResponse() bool { return m.Method == "" && len(m.ID) > 0 }
func (m message) isRequest() bool  { return m.Method != "" && len(m.ID) > 0 }

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return e.Message }

// JSON-RPC error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

type implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      implementation `json:"clientInfo"`
}

type initializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	Capabilities    struct {
		Tools     *struct{} `json:"tools,omitempty"`
		Resources *struct{} `json:"resources,omitempty"`
		Prompts   *struct{} `json:"prompts,omitempty"`
		Logging   *struct{} `json:"logging,omitempty"`
	} `json:"capabilities"`
	ServerInfo   implementation `json:"serverInfo"`
	Instructions string         `json:"instructions,omitempty"`
}

// ToolAnnotations are a server's hints about what a tool does.
type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// RemoteTool is a tool as a server lists it.
type RemoteTool struct {
	Name        string           `json:"name"`
	Title       string           `json:"title,omitempty"`
	Description string           `json:"description,omitempty"`
	InputSchema json.RawMessage  `json:"inputSchema,omitempty"`
	Annotations *ToolAnnotations `json:"annotations,omitempty"`
}

type listToolsResult struct {
	Tools      []RemoteTool `json:"tools"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

// Content is one item of a tool result or prompt message.
type Content struct {
	Type        string        `json:"type"`
	Text        string        `json:"text,omitempty"`
	MimeType    string        `json:"mimeType,omitempty"`
	Data        string        `json:"data,omitempty"`
	URI         string        `json:"uri,omitempty"`
	Name        string        `json:"name,omitempty"`
	Title       string        `json:"title,omitempty"`
	Description string        `json:"description,omitempty"`
	Resource    *ResourceText `json:"resource,omitempty"`
}

// ResourceText is the contents of a resource.
type ResourceText struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

type callToolResult struct {
	Content           []Content       `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

// Resource is a piece of context a server offers to read.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type listResourcesResult struct {
	Resources  []Resource `json:"resources"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

type readResourceResult struct {
	Contents []ResourceText `json:"contents"`
}

// PromptArgument is one value a prompt template takes.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Prompt is a ready-made request a server offers.
type Prompt struct {
	Name        string           `json:"name"`
	Title       string           `json:"title,omitempty"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

type listPromptsResult struct {
	Prompts    []Prompt `json:"prompts"`
	NextCursor string   `json:"nextCursor,omitempty"`
}

type promptMessage struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
}

type getPromptResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []promptMessage `json:"messages"`
}

type createMessageParams struct {
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	SystemPrompt string `json:"systemPrompt,omitempty"`
	MaxTokens    int    `json:"maxTokens"`
}

type logMessageParams struct {
	Level  string          `json:"level"`
	Logger string          `json:"logger,omitempty"`
	Data   json.RawMessage `json:"data"`
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mcp: marshal %T: %v", v, err))
	}
	return raw
}
