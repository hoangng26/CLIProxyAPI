package claude

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	translatorcommon "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/common"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type claudeStreamState struct {
	MessageID         string
	Model             string
	MessageStarted    bool
	MessageStopped    bool
	OpenBlock         string // "", "thinking", "text", "tool_use"
	OpenIndex         int
	NextIndex         int
	SawToolUse        bool
	ToolIndexByID     map[string]int
	InputTokens       int64
	OutputTokens      int64
	HasUsage          bool
	PendingStopReason string
}

func newClaudeStreamState(model string) *claudeStreamState {
	if model == "" {
		model = "commandcode"
	}
	return &claudeStreamState{
		MessageID:     fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		Model:         model,
		ToolIndexByID: make(map[string]int),
	}
}

func ensureClaudeStreamState(param *any, model string) *claudeStreamState {
	if param == nil {
		return newClaudeStreamState(model)
	}
	if *param == nil {
		s := newClaudeStreamState(model)
		*param = s
		return s
	}
	if s, ok := (*param).(*claudeStreamState); ok {
		return s
	}
	s := newClaudeStreamState(model)
	*param = s
	return s
}

func isRecognizedCommandCodeEvent(eventType string) bool {
	switch eventType {
	case "text-delta", "reasoning-delta", "tool-input-start", "tool-input-delta", "tool-call", "finish-step", "finish", "error":
		return true
	default:
		return false
	}
}

func mapClaudeStopReason(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "tool-use", "tool_use", "tool_calls", "tool-calls":
		return "tool_use"
	case "length", "max_tokens", "max-tokens":
		return "max_tokens"
	default:
		return "end_turn"
	}
}

func mergeClaudeUsage(state *claudeStreamState, usage gjson.Result) {
	if !usage.Exists() {
		return
	}
	state.HasUsage = true
	if v := usage.Get("promptTokens"); v.Exists() {
		state.InputTokens = v.Int()
	} else if v := usage.Get("prompt_tokens"); v.Exists() {
		state.InputTokens = v.Int()
	} else if v := usage.Get("inputTokens"); v.Exists() {
		state.InputTokens = v.Int()
	} else if v := usage.Get("input_tokens"); v.Exists() {
		state.InputTokens = v.Int()
	}
	if v := usage.Get("completionTokens"); v.Exists() {
		state.OutputTokens = v.Int()
	} else if v := usage.Get("completion_tokens"); v.Exists() {
		state.OutputTokens = v.Int()
	} else if v := usage.Get("outputTokens"); v.Exists() {
		state.OutputTokens = v.Int()
	} else if v := usage.Get("output_tokens"); v.Exists() {
		state.OutputTokens = v.Int()
	}
}

func toolCallID(root gjson.Result) string {
	return firstNonEmpty(root.Get("id").String(), root.Get("toolCallId").String())
}

func commandCodeErrorMessage(root gjson.Result) string {
	errVal := root.Get("error")
	if !errVal.Exists() {
		errVal = root.Get("message")
	}
	errStr := errVal.String()
	if errVal.IsObject() || errVal.IsArray() {
		errStr = errVal.Raw
	}
	if errStr == "" {
		return "unknown"
	}
	return errStr
}

func toolCallInputJSON(root gjson.Result) string {
	in := root.Get("input")
	if !in.Exists() {
		return "{}"
	}
	if in.Type == gjson.String {
		if s := in.String(); s != "" {
			return s
		}
		return "{}"
	}
	if in.Raw != "" {
		return in.Raw
	}
	return "{}"
}

func emitSSE(event string, payload []byte) []byte {
	return translatorcommon.AppendSSEEventBytes(nil, event, payload, 2)
}

func emitMessageStart(state *claudeStreamState) [][]byte {
	if state.MessageStarted {
		return nil
	}
	payload := []byte(`{"type":"message_start","message":{"id":"","type":"message","role":"assistant","content":[],"model":"","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}`)
	payload, _ = sjson.SetBytes(payload, "message.id", state.MessageID)
	payload, _ = sjson.SetBytes(payload, "message.model", state.Model)
	state.MessageStarted = true
	return [][]byte{emitSSE("message_start", payload)}
}

func stopOpenBlock(state *claudeStreamState) [][]byte {
	if state.OpenBlock == "" {
		return nil
	}
	payload := []byte(`{"type":"content_block_stop","index":0}`)
	payload, _ = sjson.SetBytes(payload, "index", state.OpenIndex)
	state.OpenBlock = ""
	return [][]byte{emitSSE("content_block_stop", payload)}
}

func startContentBlock(state *claudeStreamState, kind string, block []byte) [][]byte {
	out := stopOpenBlock(state)
	idx := state.NextIndex
	state.NextIndex++
	state.OpenBlock = kind
	state.OpenIndex = idx
	payload := []byte(`{"type":"content_block_start","index":0,"content_block":{}}`)
	payload, _ = sjson.SetBytes(payload, "index", idx)
	payload, _ = sjson.SetRawBytes(payload, "content_block", block)
	return append(out, emitSSE("content_block_start", payload))
}

func ensureOpenBlock(state *claudeStreamState, kind string) [][]byte {
	if state.OpenBlock == kind {
		return nil
	}
	var block []byte
	switch kind {
	case "thinking":
		block = []byte(`{"type":"thinking","thinking":""}`)
	default:
		block = []byte(`{"type":"text","text":""}`)
	}
	return startContentBlock(state, kind, block)
}

func emitContentDelta(state *claudeStreamState, deltaType, field, value string) [][]byte {
	if value == "" {
		return nil
	}
	payload := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":""}}`)
	payload, _ = sjson.SetBytes(payload, "index", state.OpenIndex)
	payload, _ = sjson.SetBytes(payload, "delta.type", deltaType)
	payload, _ = sjson.SetBytes(payload, "delta."+field, value)
	return [][]byte{emitSSE("content_block_delta", payload)}
}

func startToolUse(state *claudeStreamState, id, name string) [][]byte {
	block := []byte(`{"type":"tool_use","id":"","name":"","input":{}}`)
	block, _ = sjson.SetBytes(block, "id", id)
	block, _ = sjson.SetBytes(block, "name", name)
	out := startContentBlock(state, "tool_use", block)
	state.ToolIndexByID[id] = state.OpenIndex
	state.SawToolUse = true
	return out
}

func emitInputJSONDelta(index int, partial string) [][]byte {
	payload := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`)
	payload, _ = sjson.SetBytes(payload, "index", index)
	payload, _ = sjson.SetBytes(payload, "delta.partial_json", partial)
	return [][]byte{emitSSE("content_block_delta", payload)}
}

func emitMessageDeltaAndStop(state *claudeStreamState) [][]byte {
	if state.MessageStopped {
		return nil
	}
	reason := state.PendingStopReason
	if reason == "" {
		reason = "end_turn"
	}
	payload := []byte(`{"type":"message_delta","delta":{"stop_reason":"","stop_sequence":null},"usage":{"input_tokens":0,"output_tokens":0}}`)
	payload, _ = sjson.SetBytes(payload, "delta.stop_reason", reason)
	payload, _ = sjson.SetBytes(payload, "usage.input_tokens", state.InputTokens)
	payload, _ = sjson.SetBytes(payload, "usage.output_tokens", state.OutputTokens)
	state.MessageStopped = true
	return [][]byte{
		emitSSE("message_delta", payload),
		emitSSE("message_stop", []byte(`{"type":"message_stop"}`)),
	}
}

func finishClaudeStream(state *claudeStreamState) [][]byte {
	out := stopOpenBlock(state)
	return append(out, emitMessageDeltaAndStop(state)...)
}

// ConvertCommandCodeResponseToClaude converts one CommandCode NDJSON event line
// into zero or more Claude SSE frames.
func ConvertCommandCodeResponseToClaude(_ context.Context, modelName string, _, _, rawJSON []byte, param *any) [][]byte {
	line := bytes.TrimSpace(rawJSON)
	if len(line) == 0 {
		return nil
	}
	if bytes.HasPrefix(line, []byte("data:")) {
		line = bytes.TrimSpace(line[5:])
	}
	if len(line) == 0 {
		return nil
	}

	root := gjson.ParseBytes(line)
	if !root.IsObject() {
		return nil
	}
	eventType := root.Get("type").String()
	if eventType == "" || !isRecognizedCommandCodeEvent(eventType) {
		return nil
	}

	if param != nil {
		if s, ok := (*param).(*claudeStreamState); ok && s != nil && s.MessageStopped {
			return nil
		}
	}

	state := ensureClaudeStreamState(param, modelName)
	if modelName != "" {
		state.Model = modelName
	}

	out := emitMessageStart(state)

	switch eventType {
	case "reasoning-delta":
		text := firstNonEmpty(root.Get("text").String(), root.Get("delta").String())
		out = append(out, ensureOpenBlock(state, "thinking")...)
		out = append(out, emitContentDelta(state, "thinking_delta", "thinking", text)...)

	case "text-delta":
		text := firstNonEmpty(root.Get("text").String(), root.Get("delta").String())
		out = append(out, ensureOpenBlock(state, "text")...)
		out = append(out, emitContentDelta(state, "text_delta", "text", text)...)

	case "tool-input-start":
		id := toolCallID(root)
		if id == "" {
			id = fmt.Sprintf("call_%d", state.NextIndex)
		}
		if _, seen := state.ToolIndexByID[id]; seen {
			return out
		}
		name := firstNonEmpty(root.Get("toolName").String(), root.Get("name").String())
		out = append(out, startToolUse(state, id, name)...)

	case "tool-input-delta":
		id := toolCallID(root)
		idx, ok := state.ToolIndexByID[id]
		if !ok {
			return out
		}
		partial := firstNonEmpty(root.Get("delta").String(), root.Get("inputTextDelta").String())
		if partial == "" {
			return out
		}
		out = append(out, emitInputJSONDelta(idx, partial)...)

	case "tool-call":
		id := firstNonEmpty(root.Get("toolCallId").String(), root.Get("id").String())
		if id == "" {
			id = fmt.Sprintf("call_%d", state.NextIndex)
		}
		if _, seen := state.ToolIndexByID[id]; seen {
			return out
		}
		name := firstNonEmpty(root.Get("toolName").String(), root.Get("name").String())
		out = append(out, startToolUse(state, id, name)...)
		out = append(out, emitInputJSONDelta(state.OpenIndex, toolCallInputJSON(root))...)
		out = append(out, stopOpenBlock(state)...)

	case "finish-step":
		if fr := root.Get("finishReason"); fr.Exists() {
			state.PendingStopReason = mapClaudeStopReason(fr.String())
		}
		mergeClaudeUsage(state, root.Get("usage"))

	case "finish":
		if state.PendingStopReason == "" {
			state.PendingStopReason = mapClaudeStopReason(root.Get("finishReason").String())
		}
		usage := root.Get("totalUsage")
		if !usage.Exists() {
			usage = root.Get("usage")
		}
		mergeClaudeUsage(state, usage)
		if state.SawToolUse && state.PendingStopReason == "end_turn" {
			state.PendingStopReason = "tool_use"
		}
		out = append(out, finishClaudeStream(state)...)

	case "error":
		msg := "\n\n[CommandCode error: " + commandCodeErrorMessage(root) + "]"
		out = append(out, ensureOpenBlock(state, "text")...)
		out = append(out, emitContentDelta(state, "text_delta", "text", msg)...)
		state.PendingStopReason = "end_turn"
		out = append(out, finishClaudeStream(state)...)
	}

	return out
}
