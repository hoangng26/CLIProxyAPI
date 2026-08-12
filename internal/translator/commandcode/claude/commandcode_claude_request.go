// Package claude translates Claude /v1/messages requests to CommandCode format.
package claude

import (
	"encoding/json"
	"strings"

	ccchat "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/commandcode/openai/chat-completions"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const defaultMaxTokens = 32000
const defaultTemperature = 0.3

// ConvertClaudeRequestToCommandCode maps a Claude /v1/messages body into a
// CommandCode /alpha/generate envelope.
func ConvertClaudeRequestToCommandCode(modelName string, rawJSON []byte, stream bool) []byte {
	out := ccchat.BuildCommandCodeEnvelope(modelName, stream)
	if !json.Valid(rawJSON) {
		out, _ = sjson.SetRawBytes(out, "params.messages", []byte("[]"))
		return out
	}

	root := gjson.ParseBytes(rawJSON)

	maxTokens := int64(defaultMaxTokens)
	if v := root.Get("max_tokens"); v.Exists() {
		maxTokens = v.Int()
	}
	out, _ = sjson.SetBytes(out, "params.max_tokens", maxTokens)

	temp := defaultTemperature
	if v := root.Get("temperature"); v.Exists() {
		temp = v.Float()
	}
	out, _ = sjson.SetBytes(out, "params.temperature", temp)

	if v := root.Get("top_p"); v.Exists() {
		out, _ = sjson.SetBytes(out, "params.top_p", v.Float())
	}

	if system := convertClaudeSystem(root.Get("system")); system != "" {
		out, _ = sjson.SetBytes(out, "params.system", system)
	}

	messages := convertClaudeMessages(root.Get("messages"))
	out, _ = sjson.SetRawBytes(out, "params.messages", messages)

	if tools := ccchat.ConvertTools(root.Get("tools")); len(tools) > 0 {
		out, _ = sjson.SetRawBytes(out, "params.tools", tools)
	}

	return out
}

func convertClaudeSystem(system gjson.Result) string {
	if !system.Exists() || system.Type == gjson.Null {
		return ""
	}
	if system.Type == gjson.String {
		return system.String()
	}
	if !system.IsArray() {
		return ""
	}
	var parts []string
	system.ForEach(func(_, block gjson.Result) bool {
		if block.Type == gjson.String {
			parts = append(parts, block.String())
			return true
		}
		if t := block.Get("text"); t.Exists() && t.Type == gjson.String {
			parts = append(parts, t.String())
		}
		return true
	})
	return strings.Join(parts, "\n\n")
}

func convertClaudeMessages(messages gjson.Result) []byte {
	out := []byte("[]")
	if !messages.Exists() || !messages.IsArray() {
		return out
	}

	toolNameByCallID := make(map[string]string)
	sawUserOrAssistant := false
	emittedFromUserOrAssistant := false

	messages.ForEach(func(_, m gjson.Result) bool {
		role := m.Get("role").String()
		switch role {
		case "assistant":
			sawUserOrAssistant = true
			content, names := convertAssistantContent(m.Get("content"))
			for id, name := range names {
				toolNameByCallID[id] = name
			}
			if content == nil {
				return true
			}
			msg := []byte(`{"role":"assistant"}`)
			msg, _ = sjson.SetRawBytes(msg, "content", content)
			out, _ = sjson.SetRawBytes(out, "-1", msg)
			emittedFromUserOrAssistant = true
		case "user":
			sawUserOrAssistant = true
			userMsgs, toolMsgs := convertUserContent(m.Get("content"), toolNameByCallID)
			if len(userMsgs) == 0 && len(toolMsgs) == 0 {
				return true
			}
			for _, msg := range userMsgs {
				out, _ = sjson.SetRawBytes(out, "-1", msg)
			}
			for _, msg := range toolMsgs {
				out, _ = sjson.SetRawBytes(out, "-1", msg)
			}
			emittedFromUserOrAssistant = true
		}
		return true
	})

	if sawUserOrAssistant && !emittedFromUserOrAssistant {
		empty := []byte(`{"role":"user","content":[{"type":"text","text":""}]}`)
		out, _ = sjson.SetRawBytes(out, "-1", empty)
	}
	return out
}

func convertAssistantContent(content gjson.Result) (raw []byte, toolNames map[string]string) {
	toolNames = make(map[string]string)
	if !content.Exists() || content.Type == gjson.Null {
		return nil, toolNames
	}

	out := []byte("[]")
	count := 0

	appendText := func(text string) {
		b := []byte(`{"type":"text"}`)
		b, _ = sjson.SetBytes(b, "text", text)
		out, _ = sjson.SetRawBytes(out, "-1", b)
		count++
	}

	if content.Type == gjson.String {
		appendText(content.String())
		return out, toolNames
	}
	if !content.IsArray() {
		return nil, toolNames
	}

	content.ForEach(func(_, part gjson.Result) bool {
		if part.Type == gjson.String {
			appendText(part.String())
			return true
		}
		if shouldDropClaudePart(part) {
			return true
		}
		switch part.Get("type").String() {
		case "text":
			appendText(part.Get("text").String())
		case "tool_use":
			id := strings.TrimSpace(part.Get("id").String())
			name := strings.TrimSpace(part.Get("name").String())
			if id != "" && name != "" {
				toolNames[id] = name
			}
			block := []byte(`{"type":"tool-call"}`)
			block, _ = sjson.SetBytes(block, "toolCallId", id)
			block, _ = sjson.SetBytes(block, "toolName", name)
			if input := part.Get("input"); input.Exists() && input.IsObject() {
				block, _ = sjson.SetRawBytes(block, "input", []byte(input.Raw))
			} else {
				block, _ = sjson.SetRawBytes(block, "input", []byte("{}"))
			}
			out, _ = sjson.SetRawBytes(out, "-1", block)
			count++
		}
		return true
	})
	if count == 0 {
		return nil, toolNames
	}
	return out, toolNames
}

func convertUserContent(content gjson.Result, toolNameByCallID map[string]string) (userMsgs, toolMsgs [][]byte) {
	if !content.Exists() || content.Type == gjson.Null {
		return nil, nil
	}

	if content.Type == gjson.String {
		return [][]byte{newUserTextMessage(content.String())}, nil
	}
	if !content.IsArray() {
		return nil, nil
	}

	textBlocks := []byte("[]")
	textCount := 0
	content.ForEach(func(_, part gjson.Result) bool {
		if part.Type == gjson.String {
			b := []byte(`{"type":"text"}`)
			b, _ = sjson.SetBytes(b, "text", part.String())
			textBlocks, _ = sjson.SetRawBytes(textBlocks, "-1", b)
			textCount++
			return true
		}
		if shouldDropClaudePart(part) {
			return true
		}
		switch part.Get("type").String() {
		case "text":
			b := []byte(`{"type":"text"}`)
			b, _ = sjson.SetBytes(b, "text", part.Get("text").String())
			textBlocks, _ = sjson.SetRawBytes(textBlocks, "-1", b)
			textCount++
		case "tool_result":
			toolMsgs = append(toolMsgs, newToolResultMessage(part, toolNameByCallID))
		}
		return true
	})

	if textCount > 0 {
		msg := []byte(`{"role":"user"}`)
		msg, _ = sjson.SetRawBytes(msg, "content", textBlocks)
		userMsgs = append(userMsgs, msg)
	}
	return userMsgs, toolMsgs
}

func newUserTextMessage(text string) []byte {
	msg := []byte(`{"role":"user","content":[{"type":"text"}]}`)
	msg, _ = sjson.SetBytes(msg, "content.0.text", text)
	return msg
}

func newToolResultMessage(part gjson.Result, toolNameByCallID map[string]string) []byte {
	toolCallID := firstNonEmpty(
		strings.TrimSpace(part.Get("tool_use_id").String()),
		strings.TrimSpace(part.Get("tool_call_id").String()),
	)
	toolName := firstNonEmpty(strings.TrimSpace(part.Get("name").String()), toolNameByCallID[toolCallID])
	block := []byte(`{"type":"tool-result"}`)
	block, _ = sjson.SetBytes(block, "toolCallId", toolCallID)
	block, _ = sjson.SetBytes(block, "toolName", toolName)
	block, _ = sjson.SetBytes(block, "output.type", "text")
	block, _ = sjson.SetBytes(block, "output.value", flattenToolResultValue(part.Get("content")))
	msg := []byte(`{"role":"tool","content":[]}`)
	msg, _ = sjson.SetRawBytes(msg, "content.-1", block)
	return msg
}

func flattenToolResultValue(content gjson.Result) string {
	if !content.Exists() || content.Type == gjson.Null {
		return ""
	}
	if content.Type == gjson.String {
		return content.String()
	}
	if content.IsArray() {
		var parts []string
		content.ForEach(func(_, p gjson.Result) bool {
			if p.Type == gjson.String {
				parts = append(parts, p.String())
			} else if t := p.Get("text"); t.Exists() && t.Type == gjson.String {
				parts = append(parts, t.String())
			}
			return true
		})
		return strings.Join(parts, "")
	}
	return content.String()
}

func shouldDropClaudePart(part gjson.Result) bool {
	switch part.Get("type").String() {
	case "thinking", "redacted_thinking", "image", "image_url":
		return true
	}
	if part.Get("source.data").Exists() || part.Get("source.base64").Exists() {
		return true
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
