// Package chat_completions translates OpenAI Chat Completions requests to CommandCode format.
package chat_completions

import (
	"encoding/json"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const defaultMaxTokens = 32000
const defaultTemperature = 0.3

// ConvertOpenAIRequestToCommandCode transforms an OpenAI chat-completions body into a
// CommandCode /alpha/generate envelope.
func ConvertOpenAIRequestToCommandCode(modelName string, rawJSON []byte, stream bool) []byte {
	root := gjson.ParseBytes(rawJSON)

	out := []byte(`{"threadId":"","memory":"","config":{},"params":{}}`)
	out, _ = sjson.SetBytes(out, "threadId", uuid.NewString())
	out, _ = sjson.SetBytes(out, "memory", "")

	out, _ = sjson.SetBytes(out, "config.workingDir", "")
	out, _ = sjson.SetBytes(out, "config.date", time.Now().UTC().Format("2006-01-02"))
	out, _ = sjson.SetBytes(out, "config.environment", runtime.GOOS)
	out, _ = sjson.SetRawBytes(out, "config.structure", []byte("[]"))
	out, _ = sjson.SetBytes(out, "config.isGitRepo", false)
	out, _ = sjson.SetBytes(out, "config.currentBranch", "")
	out, _ = sjson.SetBytes(out, "config.mainBranch", "")
	out, _ = sjson.SetBytes(out, "config.gitStatus", "")
	out, _ = sjson.SetRawBytes(out, "config.recentCommits", []byte("[]"))

	out, _ = sjson.SetBytes(out, "params.model", modelName)
	out, _ = sjson.SetBytes(out, "params.stream", stream)

	maxTokens := int64(defaultMaxTokens)
	if v := root.Get("max_tokens"); v.Exists() {
		maxTokens = v.Int()
	} else if v := root.Get("max_output_tokens"); v.Exists() {
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

	messages, system := convertMessages(root.Get("messages"))
	out, _ = sjson.SetRawBytes(out, "params.messages", messages)
	if system != "" {
		out, _ = sjson.SetBytes(out, "params.system", system)
	}

	if tools := convertTools(root.Get("tools")); len(tools) > 0 {
		out, _ = sjson.SetRawBytes(out, "params.tools", tools)
	}

	return out
}

func convertMessages(messages gjson.Result) (raw []byte, system string) {
	if !messages.IsArray() {
		return []byte("[]"), ""
	}

	out := []byte("[]")
	var systemParts []string
	// OpenCode/AI SDK clients often omit tool message "name". CommandCode/AI SDK
	// tool-result parts require toolName, so backfill from the matching tool_call.
	toolNameByCallID := make(map[string]string)

	messages.ForEach(func(_, m gjson.Result) bool {
		role := m.Get("role").String()
		switch role {
		case "system":
			t := flattenText(m.Get("content"))
			if t != "" {
				systemParts = append(systemParts, t)
			}
		case "tool":
			value := flattenText(m.Get("content"))
			block := []byte(`{}`)
			block, _ = sjson.SetBytes(block, "type", "tool-result")
			toolCallID := firstNonEmpty(
				strings.TrimSpace(m.Get("tool_call_id").String()),
				strings.TrimSpace(m.Get("call_id").String()),
			)
			toolName := firstNonEmpty(strings.TrimSpace(m.Get("name").String()), toolNameByCallID[toolCallID])
			block, _ = sjson.SetBytes(block, "toolCallId", toolCallID)
			block, _ = sjson.SetBytes(block, "toolName", toolName)
			block, _ = sjson.SetBytes(block, "output.type", "text")
			block, _ = sjson.SetBytes(block, "output.value", value)
			msg := []byte(`{"role":"tool","content":[]}`)
			msg, _ = sjson.SetRawBytes(msg, "content.-1", block)
			out, _ = sjson.SetRawBytes(out, "-1", msg)
		case "assistant":
			content := []byte("[]")
			if text := flattenText(m.Get("content")); text != "" {
				tb := []byte(`{"type":"text"}`)
				tb, _ = sjson.SetBytes(tb, "text", text)
				content, _ = sjson.SetRawBytes(content, "-1", tb)
			}
			if tcs := m.Get("tool_calls"); tcs.IsArray() {
				tcs.ForEach(func(_, tc gjson.Result) bool {
					fn := tc.Get("function")
					block := []byte(`{"type":"tool-call"}`)
					toolCallID := firstNonEmpty(
						strings.TrimSpace(tc.Get("id").String()),
						strings.TrimSpace(tc.Get("tool_call_id").String()),
						strings.TrimSpace(tc.Get("call_id").String()),
					)
					toolName := strings.TrimSpace(fn.Get("name").String())
					if toolCallID != "" && toolName != "" {
						toolNameByCallID[toolCallID] = toolName
					}
					block, _ = sjson.SetBytes(block, "toolCallId", toolCallID)
					block, _ = sjson.SetBytes(block, "toolName", toolName)
					block, _ = sjson.SetRawBytes(block, "input", safeParseJSON(fn.Get("arguments").String()))
					content, _ = sjson.SetRawBytes(content, "-1", block)
					return true
				})
			}
			if len(gjson.ParseBytes(content).Array()) == 0 {
				content = []byte(`[{"type":"text","text":""}]`)
			}
			msg := []byte(`{"role":"assistant"}`)
			msg, _ = sjson.SetRawBytes(msg, "content", content)
			out, _ = sjson.SetRawBytes(out, "-1", msg)
		default: // user and unknown → user
			msg := []byte(`{"role":"user"}`)
			msg, _ = sjson.SetRawBytes(msg, "content", toContentBlocks(m.Get("content")))
			out, _ = sjson.SetRawBytes(out, "-1", msg)
		}
		return true
	})

	if len(systemParts) > 0 {
		system = joinDoubleNewline(systemParts)
	}
	return out, system
}

func convertTools(tools gjson.Result) []byte {
	if !tools.IsArray() || len(tools.Array()) == 0 {
		return nil
	}
	out := []byte("[]")
	count := 0
	tools.ForEach(func(_, t gjson.Result) bool {
		var name, desc string
		var schema []byte
		if t.Get("type").String() == "function" && t.Get("function").Exists() {
			fn := t.Get("function")
			name = fn.Get("name").String()
			desc = fn.Get("description").String()
			if p := fn.Get("parameters"); p.Exists() {
				schema = []byte(p.Raw)
			} else {
				schema = []byte(`{"type":"object"}`)
			}
		} else if t.Get("name").Exists() {
			name = t.Get("name").String()
			desc = t.Get("description").String()
			if s := t.Get("input_schema"); s.Exists() {
				schema = []byte(s.Raw)
			} else if p := t.Get("parameters"); p.Exists() {
				schema = []byte(p.Raw)
			} else {
				return true
			}
		} else {
			return true
		}
		item := []byte(`{}`)
		item, _ = sjson.SetBytes(item, "name", name)
		item, _ = sjson.SetBytes(item, "description", desc)
		item, _ = sjson.SetRawBytes(item, "input_schema", schema)
		out, _ = sjson.SetRawBytes(out, "-1", item)
		count++
		return true
	})
	if count == 0 {
		return nil
	}
	return out
}

func flattenText(content gjson.Result) string {
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
		return joinNewline(parts)
	}
	return content.String()
}

func toContentBlocks(content gjson.Result) []byte {
	if !content.Exists() || content.Type == gjson.Null {
		return []byte(`[{"type":"text","text":""}]`)
	}
	if content.Type == gjson.String {
		b := []byte(`[{"type":"text"}]`)
		b, _ = sjson.SetBytes(b, "0.text", content.String())
		return b
	}
	if content.IsArray() {
		out := []byte("[]")
		count := 0
		content.ForEach(func(_, part gjson.Result) bool {
			if part.Type == gjson.String {
				b := []byte(`{"type":"text"}`)
				b, _ = sjson.SetBytes(b, "text", part.String())
				out, _ = sjson.SetRawBytes(out, "-1", b)
				count++
				return true
			}
			typ := part.Get("type").String()
			switch typ {
			case "text":
				b := []byte(`{"type":"text"}`)
				b, _ = sjson.SetBytes(b, "text", part.Get("text").String())
				out, _ = sjson.SetRawBytes(out, "-1", b)
				count++
			case "image_url", "image":
				out, _ = sjson.SetRawBytes(out, "-1", []byte(`{"type":"text","text":"[image omitted]"}`))
				count++
			default:
				if t := part.Get("text"); t.Exists() && t.Type == gjson.String {
					b := []byte(`{"type":"text"}`)
					b, _ = sjson.SetBytes(b, "text", t.String())
					out, _ = sjson.SetRawBytes(out, "-1", b)
					count++
				}
			}
			return true
		})
		if count == 0 {
			return []byte(`[{"type":"text","text":""}]`)
		}
		return out
	}
	b := []byte(`[{"type":"text"}]`)
	b, _ = sjson.SetBytes(b, "0.text", content.String())
	return b
}

func safeParseJSON(s string) []byte {
	if s == "" {
		return []byte("{}")
	}
	if json.Valid([]byte(s)) {
		return []byte(s)
	}
	return []byte("{}")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func joinNewline(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += "\n" + parts[i]
	}
	return out
}

func joinDoubleNewline(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += "\n\n" + parts[i]
	}
	return out
}
