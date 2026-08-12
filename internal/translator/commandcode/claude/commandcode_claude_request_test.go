package claude

import (
	"testing"

	"github.com/tidwall/gjson"
)

func assertZodEnvelope(t *testing.T, out []byte) {
	t.Helper()
	if gjson.GetBytes(out, "memory").Type != gjson.String {
		t.Fatalf("memory must be string: %s", out)
	}
	if !gjson.GetBytes(out, "config").IsObject() {
		t.Fatalf("config must be object: %s", out)
	}
	msgs := gjson.GetBytes(out, "params.messages")
	if !msgs.Exists() || !msgs.IsArray() {
		t.Fatalf("params.messages must be array: %s", out)
	}
}

func TestConvertClaudeRequestToCommandCode_MinimalZod(t *testing.T) {
	in := []byte(`{"model":"deepseek/deepseek-v4-pro","max_tokens":32,"messages":[{"role":"user","content":"Hello"}]}`)
	out := ConvertClaudeRequestToCommandCode("deepseek/deepseek-v4-pro", in, true)
	assertZodEnvelope(t, out)
	if gjson.GetBytes(out, "params.model").String() != "deepseek/deepseek-v4-pro" {
		t.Fatalf("params.model=%s", gjson.GetBytes(out, "params.model").Raw)
	}
	if !gjson.GetBytes(out, "params.stream").Bool() {
		t.Fatal("params.stream want true")
	}
	if gjson.GetBytes(out, "params.max_tokens").Int() != 32 {
		t.Fatalf("max_tokens=%s", gjson.GetBytes(out, "params.max_tokens").Raw)
	}
	msgs := gjson.GetBytes(out, "params.messages").Array()
	if len(msgs) != 1 || msgs[0].Get("role").String() != "user" {
		t.Fatalf("messages=%s", gjson.GetBytes(out, "params.messages").Raw)
	}
	if msgs[0].Get("content.0.text").String() != "Hello" {
		t.Fatalf("user text=%s", msgs[0].Get("content").Raw)
	}
}

func TestConvertClaudeRequestToCommandCode_SystemArray(t *testing.T) {
	in := []byte(`{
	  "system":[{"type":"text","text":"a"},{"type":"text","text":"b"}],
	  "messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]
	}`)
	out := ConvertClaudeRequestToCommandCode("m", in, false)
	if gjson.GetBytes(out, "params.system").String() != "a\n\nb" {
		t.Fatalf("system=%q", gjson.GetBytes(out, "params.system").String())
	}
}

func TestConvertClaudeRequestToCommandCode_ToolUseAndResult(t *testing.T) {
	in := []byte(`{
	  "messages":[
	    {"role":"assistant","content":[{"type":"tool_use","id":"c1","name":"ping","input":{"x":1}}]},
	    {"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"pong"}]}
	  ],
	  "tools":[{"name":"ping","description":"p","input_schema":{"type":"object"}}]
	}`)
	out := ConvertClaudeRequestToCommandCode("m", in, false)
	assertZodEnvelope(t, out)
	if gjson.GetBytes(out, "params.tools.0.name").String() != "ping" {
		t.Fatalf("tools=%s", gjson.GetBytes(out, "params.tools").Raw)
	}
	msgs := gjson.GetBytes(out, "params.messages").Array()
	if len(msgs) != 2 {
		t.Fatalf("messages=%s", gjson.GetBytes(out, "params.messages").Raw)
	}
	if msgs[0].Get("content.0.type").String() != "tool-call" {
		t.Fatalf("assistant=%s", msgs[0].Get("content").Raw)
	}
	if msgs[0].Get("content.0.toolCallId").String() != "c1" {
		t.Fatal("toolCallId")
	}
	if msgs[0].Get("content.0.input.x").Int() != 1 {
		t.Fatalf("input=%s", msgs[0].Get("content.0.input").Raw)
	}
	if msgs[1].Get("role").String() != "tool" {
		t.Fatalf("tool role=%s", msgs[1].Raw)
	}
	if msgs[1].Get("content.0.toolName").String() != "ping" {
		t.Fatalf("backfill toolName=%q", msgs[1].Get("content.0.toolName").String())
	}
	if msgs[1].Get("content.0.output.value").String() != "pong" {
		t.Fatalf("tool output=%s", msgs[1].Get("content.0").Raw)
	}
}

func TestConvertClaudeRequestToCommandCode_DropsThinkingAndImages(t *testing.T) {
	in := []byte(`{
	  "messages":[{
	    "role":"user",
	    "content":[
	      {"type":"thinking","thinking":"secret"},
	      {"type":"redacted_thinking","data":"x"},
	      {"type":"image","source":{"type":"base64","data":"abc"}},
	      {"type":"text","text":"keep"}
	    ]
	  }]
	}`)
	out := ConvertClaudeRequestToCommandCode("m", in, false)
	raw := gjson.GetBytes(out, "params.messages").Raw
	if gjson.GetBytes(out, "params.messages.0.content.#").Int() != 1 {
		t.Fatalf("want only leftover text, got %s", raw)
	}
	if gjson.GetBytes(out, "params.messages.0.content.0.text").String() != "keep" {
		t.Fatalf("text=%s", raw)
	}
}

func TestConvertClaudeRequestToCommandCode_InvalidJSONStillZodSafe(t *testing.T) {
	out := ConvertClaudeRequestToCommandCode("m", []byte(`not-json`), false)
	assertZodEnvelope(t, out)
	if len(gjson.GetBytes(out, "params.messages").Array()) != 0 {
		t.Fatalf("invalid json must yield empty messages: %s", out)
	}
}

func TestConvertClaudeRequestToCommandCode_AllPartsDroppedEmitsEmptyUser(t *testing.T) {
	in := []byte(`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"x"}}]}]}`)
	out := ConvertClaudeRequestToCommandCode("m", in, false)
	assertZodEnvelope(t, out)
	msgs := gjson.GetBytes(out, "params.messages").Array()
	if len(msgs) != 1 || msgs[0].Get("role").String() != "user" {
		t.Fatalf("want one empty user, got %s", gjson.GetBytes(out, "params.messages").Raw)
	}
}
