// Package chat_completions translates CommandCode NDJSON stream events to OpenAI chat.completion format.
package chat_completions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// commandCodeStreamState holds streaming conversion state across NDJSON lines.
type commandCodeStreamState struct {
	ResponseID    string
	Created       int64
	Model         string
	ChunkIndex    int
	ToolIndex     int
	ToolIndexByID map[string]int
	FinishReason  string
	PromptTokens  int64
	CompletionTok int64
	HasUsage      bool
}

func ensureStreamState(param *any, model string) *commandCodeStreamState {
	if param == nil {
		s := newStreamState(model)
		return s
	}
	if *param == nil {
		s := newStreamState(model)
		*param = s
		return s
	}
	if s, ok := (*param).(*commandCodeStreamState); ok {
		return s
	}
	s := newStreamState(model)
	*param = s
	return s
}

func newStreamState(model string) *commandCodeStreamState {
	m := model
	if m == "" {
		m = "commandcode"
	}
	return &commandCodeStreamState{
		ResponseID:    fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Created:       time.Now().Unix(),
		Model:         m,
		ToolIndexByID: make(map[string]int),
	}
}

func makeOpenAIChunk(state *commandCodeStreamState, deltaJSON []byte, finishReason *string) []byte {
	out := []byte(`{"id":"","object":"chat.completion.chunk","created":0,"model":"","choices":[{"index":0,"delta":{},"finish_reason":null}]}`)
	out, _ = sjson.SetBytes(out, "id", state.ResponseID)
	out, _ = sjson.SetBytes(out, "created", state.Created)
	out, _ = sjson.SetBytes(out, "model", state.Model)
	if len(deltaJSON) > 0 {
		out, _ = sjson.SetRawBytes(out, "choices.0.delta", deltaJSON)
	}
	if finishReason != nil {
		out, _ = sjson.SetBytes(out, "choices.0.finish_reason", *finishReason)
	}
	return out
}

func mapFinishReason(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "tool-calls", "tool_calls", "tool-use", "tool_use":
		return "tool_calls"
	case "length", "max_tokens", "max-tokens":
		return "length"
	case "content_filter", "content-filter":
		return "content_filter"
	case "stop", "end_turn", "end-turn", "":
		return "stop"
	default:
		return "stop"
	}
}

func applyUsage(out []byte, prompt, completion int64) []byte {
	out, _ = sjson.SetBytes(out, "usage.prompt_tokens", prompt)
	out, _ = sjson.SetBytes(out, "usage.completion_tokens", completion)
	out, _ = sjson.SetBytes(out, "usage.total_tokens", prompt+completion)
	return out
}

func mergeUsageFromEvent(state *commandCodeStreamState, usage gjson.Result) {
	if !usage.Exists() {
		return
	}
	state.HasUsage = true
	if v := usage.Get("promptTokens"); v.Exists() {
		state.PromptTokens = v.Int()
	} else if v := usage.Get("prompt_tokens"); v.Exists() {
		state.PromptTokens = v.Int()
	} else if v := usage.Get("inputTokens"); v.Exists() {
		state.PromptTokens = v.Int()
	} else if v := usage.Get("input_tokens"); v.Exists() {
		state.PromptTokens = v.Int()
	}
	if v := usage.Get("completionTokens"); v.Exists() {
		state.CompletionTok = v.Int()
	} else if v := usage.Get("completion_tokens"); v.Exists() {
		state.CompletionTok = v.Int()
	} else if v := usage.Get("outputTokens"); v.Exists() {
		state.CompletionTok = v.Int()
	} else if v := usage.Get("output_tokens"); v.Exists() {
		state.CompletionTok = v.Int()
	}
}

// ConvertCommandCodeResponseToOpenAI converts one CommandCode NDJSON event line into
// zero or more OpenAI chat.completion.chunk JSON payloads (no data: prefix).
func ConvertCommandCodeResponseToOpenAI(_ context.Context, modelName string, _, _, rawJSON []byte, param *any) [][]byte {
	line := bytes.TrimSpace(rawJSON)
	if len(line) == 0 {
		return nil
	}
	if bytes.HasPrefix(line, []byte("data:")) {
		line = bytes.TrimSpace(line[5:])
	}
	if len(line) == 0 || bytes.Equal(line, []byte("[DONE]")) {
		return nil
	}

	// Pass-through already-OpenAI chunks.
	if gjson.GetBytes(line, "object").String() == "chat.completion.chunk" {
		return [][]byte{line}
	}

	root := gjson.ParseBytes(line)
	if !root.IsObject() {
		return nil
	}
	eventType := root.Get("type").String()
	if eventType == "" {
		return nil
	}

	state := ensureStreamState(param, modelName)
	if m := root.Get("model"); m.Exists() && m.String() != "" && state.ChunkIndex == 0 && state.Model == "commandcode" {
		state.Model = m.String()
	}
	if modelName != "" {
		state.Model = modelName
	}

	switch eventType {
	case "text-delta":
		text := root.Get("text").String()
		if text == "" {
			text = root.Get("delta").String()
		}
		if text == "" {
			return nil
		}
		delta := []byte(`{}`)
		if state.ChunkIndex == 0 {
			delta, _ = sjson.SetBytes(delta, "role", "assistant")
		}
		delta, _ = sjson.SetBytes(delta, "content", text)
		state.ChunkIndex++
		return [][]byte{makeOpenAIChunk(state, delta, nil)}

	case "reasoning-delta":
		text := root.Get("text").String()
		if text == "" {
			return nil
		}
		delta := []byte(`{}`)
		if state.ChunkIndex == 0 {
			delta, _ = sjson.SetBytes(delta, "role", "assistant")
		}
		delta, _ = sjson.SetBytes(delta, "reasoning_content", text)
		state.ChunkIndex++
		return [][]byte{makeOpenAIChunk(state, delta, nil)}

	case "tool-input-start":
		id := root.Get("id").String()
		if id == "" {
			id = root.Get("toolCallId").String()
		}
		if id == "" {
			id = fmt.Sprintf("call_%d", state.ToolIndex)
		}
		idx, ok := state.ToolIndexByID[id]
		if !ok {
			idx = state.ToolIndex
			state.ToolIndex++
			state.ToolIndexByID[id] = idx
		}
		delta := []byte(`{}`)
		if state.ChunkIndex == 0 {
			delta, _ = sjson.SetBytes(delta, "role", "assistant")
		}
		delta, _ = sjson.SetBytes(delta, "tool_calls.0.index", idx)
		delta, _ = sjson.SetBytes(delta, "tool_calls.0.id", id)
		delta, _ = sjson.SetBytes(delta, "tool_calls.0.type", "function")
		delta, _ = sjson.SetBytes(delta, "tool_calls.0.function.name", root.Get("toolName").String())
		delta, _ = sjson.SetBytes(delta, "tool_calls.0.function.arguments", "")
		state.ChunkIndex++
		return [][]byte{makeOpenAIChunk(state, delta, nil)}

	case "tool-input-delta":
		id := root.Get("id").String()
		if id == "" {
			id = root.Get("toolCallId").String()
		}
		idx, ok := state.ToolIndexByID[id]
		if !ok {
			return nil
		}
		args := root.Get("delta").String()
		if args == "" {
			args = root.Get("inputTextDelta").String()
		}
		delta := []byte(`{}`)
		delta, _ = sjson.SetBytes(delta, "tool_calls.0.index", idx)
		delta, _ = sjson.SetBytes(delta, "tool_calls.0.function.arguments", args)
		return [][]byte{makeOpenAIChunk(state, delta, nil)}

	case "tool-call":
		id := root.Get("toolCallId").String()
		if id == "" {
			id = root.Get("id").String()
		}
		if id == "" {
			id = fmt.Sprintf("call_%d", state.ToolIndex)
		}
		if _, seen := state.ToolIndexByID[id]; seen {
			// Already streamed via tool-input-*; skip consolidated event.
			return nil
		}
		idx := state.ToolIndex
		state.ToolIndex++
		state.ToolIndexByID[id] = idx
		argsStr := ""
		if in := root.Get("input"); in.Exists() {
			if in.Type == gjson.String {
				argsStr = in.String()
			} else {
				argsStr = in.Raw
			}
		}
		if argsStr == "" {
			argsStr = "{}"
		}
		delta := []byte(`{}`)
		if state.ChunkIndex == 0 {
			delta, _ = sjson.SetBytes(delta, "role", "assistant")
		}
		delta, _ = sjson.SetBytes(delta, "tool_calls.0.index", idx)
		delta, _ = sjson.SetBytes(delta, "tool_calls.0.id", id)
		delta, _ = sjson.SetBytes(delta, "tool_calls.0.type", "function")
		delta, _ = sjson.SetBytes(delta, "tool_calls.0.function.name", root.Get("toolName").String())
		delta, _ = sjson.SetBytes(delta, "tool_calls.0.function.arguments", argsStr)
		state.ChunkIndex++
		return [][]byte{makeOpenAIChunk(state, delta, nil)}

	case "finish-step":
		if fr := root.Get("finishReason"); fr.Exists() {
			state.FinishReason = mapFinishReason(fr.String())
		}
		mergeUsageFromEvent(state, root.Get("usage"))
		return nil

	case "finish":
		fr := state.FinishReason
		if fr == "" {
			fr = mapFinishReason(root.Get("finishReason").String())
			if fr == "" {
				fr = "stop"
			}
		}
		totalUsage := root.Get("totalUsage")
		if !totalUsage.Exists() {
			totalUsage = root.Get("usage")
		}
		mergeUsageFromEvent(state, totalUsage)
		if len(state.ToolIndexByID) > 0 && (fr == "" || fr == "stop") {
			fr = "tool_calls"
		}
		chunk := makeOpenAIChunk(state, []byte(`{}`), &fr)
		if state.HasUsage {
			chunk = applyUsage(chunk, state.PromptTokens, state.CompletionTok)
		}
		return [][]byte{chunk}

	case "error":
		errVal := root.Get("error")
		if !errVal.Exists() {
			errVal = root.Get("message")
		}
		errStr := errVal.String()
		if errVal.IsObject() || errVal.IsArray() {
			errStr = errVal.Raw
		}
		if errStr == "" {
			errStr = "unknown"
		}
		delta := []byte(`{}`)
		if state.ChunkIndex == 0 {
			delta, _ = sjson.SetBytes(delta, "role", "assistant")
		}
		delta, _ = sjson.SetBytes(delta, "content", "\n\n[CommandCode error: "+errStr+"]")
		state.ChunkIndex++
		fr := "stop"
		return [][]byte{
			makeOpenAIChunk(state, delta, nil),
			makeOpenAIChunk(state, []byte(`{}`), &fr),
		}

	default:
		// start, start-step, reasoning-start/end, text-start/end, metadata, etc.
		return nil
	}
}

// ConvertCommandCodeResponseToOpenAINonStream folds a full NDJSON body into one
// OpenAI chat.completion JSON object.
func ConvertCommandCodeResponseToOpenAINonStream(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	var param any
	var contentParts []string
	var reasoningParts []string
	type toolAcc struct {
		ID, Name, Args string
	}
	toolsByIdx := map[int]*toolAcc{}
	var finishReason string
	var promptTok, completionTok int64
	hasUsage := false
	responseID := ""
	created := int64(0)
	model := modelName

	lines := bytes.Split(rawJSON, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		chunks := ConvertCommandCodeResponseToOpenAI(ctx, modelName, originalRequestRawJSON, requestRawJSON, line, &param)
		if st, ok := param.(*commandCodeStreamState); ok && st != nil {
			if responseID == "" {
				responseID = st.ResponseID
				created = st.Created
				model = st.Model
			}
			if st.FinishReason != "" {
				finishReason = st.FinishReason
			}
			if st.HasUsage {
				hasUsage = true
				promptTok = st.PromptTokens
				completionTok = st.CompletionTok
			}
		}
		for _, ch := range chunks {
			root := gjson.ParseBytes(ch)
			if fr := root.Get("choices.0.finish_reason"); fr.Exists() && fr.Type != gjson.Null {
				finishReason = fr.String()
			}
			if u := root.Get("usage"); u.Exists() {
				hasUsage = true
				promptTok = u.Get("prompt_tokens").Int()
				completionTok = u.Get("completion_tokens").Int()
			}
			delta := root.Get("choices.0.delta")
			if c := delta.Get("content"); c.Exists() && c.String() != "" {
				contentParts = append(contentParts, c.String())
			}
			if r := delta.Get("reasoning_content"); r.Exists() && r.String() != "" {
				reasoningParts = append(reasoningParts, r.String())
			}
			if tcs := delta.Get("tool_calls"); tcs.IsArray() {
				tcs.ForEach(func(_, tc gjson.Result) bool {
					idx := int(tc.Get("index").Int())
					acc, ok := toolsByIdx[idx]
					if !ok {
						acc = &toolAcc{}
						toolsByIdx[idx] = acc
					}
					if id := tc.Get("id").String(); id != "" {
						acc.ID = id
					}
					if n := tc.Get("function.name").String(); n != "" {
						acc.Name = n
					}
					if a := tc.Get("function.arguments"); a.Exists() {
						acc.Args += a.String()
					}
					return true
				})
			}
		}
	}

	out := []byte(`{"id":"","object":"chat.completion","created":0,"model":"","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`)
	if responseID == "" {
		responseID = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	}
	if created == 0 {
		created = time.Now().Unix()
	}
	if model == "" {
		model = "commandcode"
	}
	out, _ = sjson.SetBytes(out, "id", responseID)
	out, _ = sjson.SetBytes(out, "created", created)
	out, _ = sjson.SetBytes(out, "model", model)
	out, _ = sjson.SetBytes(out, "choices.0.message.content", strings.Join(contentParts, ""))
	if len(reasoningParts) > 0 {
		out, _ = sjson.SetBytes(out, "choices.0.message.reasoning_content", strings.Join(reasoningParts, ""))
	}
	if len(toolsByIdx) > 0 {
		// emit in index order
		maxIdx := -1
		for i := range toolsByIdx {
			if i > maxIdx {
				maxIdx = i
			}
		}
		n := 0
		for i := 0; i <= maxIdx; i++ {
			acc, ok := toolsByIdx[i]
			if !ok {
				continue
			}
			args := acc.Args
			if args == "" {
				args = "{}"
			}
			// validate args is JSON-ish string for clients
			if !json.Valid([]byte(args)) {
				// keep as-is; OpenAI args are a JSON string
			}
			base := fmt.Sprintf("choices.0.message.tool_calls.%d", n)
			out, _ = sjson.SetBytes(out, base+".id", acc.ID)
			out, _ = sjson.SetBytes(out, base+".type", "function")
			out, _ = sjson.SetBytes(out, base+".function.name", acc.Name)
			out, _ = sjson.SetBytes(out, base+".function.arguments", args)
			n++
		}
		if n > 0 && (finishReason == "" || finishReason == "stop") {
			finishReason = "tool_calls"
		}
	}
	if finishReason == "" {
		finishReason = "stop"
	}
	out, _ = sjson.SetBytes(out, "choices.0.finish_reason", finishReason)
	if hasUsage {
		out = applyUsage(out, promptTok, completionTok)
	}
	return out
}
