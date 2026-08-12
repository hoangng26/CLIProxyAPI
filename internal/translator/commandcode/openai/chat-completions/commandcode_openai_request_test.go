package chat_completions

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIRequestToCommandCode_SystemAndTools(t *testing.T) {
	in := []byte(`{
	  "model":"deepseek/deepseek-v4-flash",
	  "messages":[
	    {"role":"system","content":"be brief"},
	    {"role":"user","content":"hi"}
	  ],
	  "tools":[{"type":"function","function":{"name":"ping","description":"p","parameters":{"type":"object"}}}],
	  "max_tokens": 128,
	  "temperature": 0.2
	}`)
	out := ConvertOpenAIRequestToCommandCode("deepseek/deepseek-v4-flash", in, true)
	if !gjson.GetBytes(out, "params.stream").Bool() {
		t.Fatal("params.stream want true")
	}
	if gjson.GetBytes(out, "params.system").String() != "be brief" {
		t.Fatalf("system=%s", gjson.GetBytes(out, "params.system").Raw)
	}
	msgs := gjson.GetBytes(out, "params.messages")
	if !msgs.IsArray() || len(msgs.Array()) != 1 {
		t.Fatalf("messages=%s", msgs.Raw)
	}
	if msgs.Array()[0].Get("role").String() != "user" {
		t.Fatal("system must not remain in messages")
	}
	if !msgs.Array()[0].Get("content").IsArray() {
		t.Fatal("content must be array")
	}
	if gjson.GetBytes(out, "params.tools.0.name").String() != "ping" {
		t.Fatal("tools not converted")
	}
	if gjson.GetBytes(out, "threadId").String() == "" {
		t.Fatal("threadId required")
	}
}

func TestConvertOpenAIRequestToCommandCode_ToolCallsAndResults(t *testing.T) {
	in := []byte(`{
	  "messages":[
	    {"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"ping","arguments":"{\"x\":1}"}}]},
	    {"role":"tool","tool_call_id":"c1","name":"ping","content":"pong"}
	  ]
	}`)
	out := ConvertOpenAIRequestToCommandCode("m", in, false)
	msgs := gjson.GetBytes(out, "params.messages").Array()
	if len(msgs) != 2 {
		t.Fatalf("messages=%s", gjson.GetBytes(out, "params.messages").Raw)
	}
	asst := msgs[0]
	if asst.Get("content.0.type").String() != "tool-call" {
		t.Fatalf("assistant block=%s", asst.Get("content").Raw)
	}
	if asst.Get("content.0.toolCallId").String() != "c1" {
		t.Fatal("toolCallId missing")
	}
	if asst.Get("content.0.input.x").Int() != 1 {
		t.Fatalf("input=%s", asst.Get("content.0.input").Raw)
	}
	tool := msgs[1]
	if tool.Get("role").String() != "tool" {
		t.Fatal("tool role")
	}
	if tool.Get("content.0.type").String() != "tool-result" {
		t.Fatalf("tool content=%s", tool.Get("content").Raw)
	}
	if tool.Get("content.0.output.value").String() != "pong" {
		t.Fatal("tool output")
	}
	if tool.Get("content.0.toolName").String() != "ping" {
		t.Fatalf("toolName=%q", tool.Get("content.0.toolName").String())
	}
	if gjson.GetBytes(out, "params.stream").Bool() {
		t.Fatal("stream want false")
	}
}

func TestConvertOpenAIRequestToCommandCode_BackfillsToolNameAndCallID(t *testing.T) {
	// OpenCode/AI SDK style: tool messages often omit "name" and may only send call_id.
	in := []byte(`{
	  "messages":[
	    {"role":"assistant","content":null,"tool_calls":[
	      {"id":"c1","type":"function","function":{"name":"ping","arguments":"{}"}},
	      {"id":"c2","type":"function","function":{"name":"pong","arguments":"{\"y\":2}"}}
	    ]},
	    {"role":"tool","tool_call_id":"c1","content":"one"},
	    {"role":"tool","call_id":"c2","content":"two"}
	  ]
	}`)
	out := ConvertOpenAIRequestToCommandCode("m", in, false)
	msgs := gjson.GetBytes(out, "params.messages").Array()
	if len(msgs) != 3 {
		t.Fatalf("messages=%s", gjson.GetBytes(out, "params.messages").Raw)
	}

	if got := msgs[1].Get("content.0.toolCallId").String(); got != "c1" {
		t.Fatalf("tool1 toolCallId=%q", got)
	}
	if got := msgs[1].Get("content.0.toolName").String(); got != "ping" {
		t.Fatalf("tool1 toolName=%q want ping (backfilled)", got)
	}
	if got := msgs[1].Get("content.0.output.value").String(); got != "one" {
		t.Fatalf("tool1 output=%q", got)
	}

	if got := msgs[2].Get("content.0.toolCallId").String(); got != "c2" {
		t.Fatalf("tool2 toolCallId=%q want c2 from call_id fallback", got)
	}
	if got := msgs[2].Get("content.0.toolName").String(); got != "pong" {
		t.Fatalf("tool2 toolName=%q want pong (backfilled)", got)
	}
	if got := msgs[2].Get("content.0.output.value").String(); got != "two" {
		t.Fatalf("tool2 output=%q", got)
	}
}

func TestBuildCommandCodeEnvelope_ZodShape(t *testing.T) {
	out := BuildCommandCodeEnvelope("deepseek/deepseek-v4-pro", true)
	if gjson.GetBytes(out, "memory").Type != gjson.String {
		t.Fatalf("memory type=%s raw=%s", gjson.GetBytes(out, "memory").Type, out)
	}
	if !gjson.GetBytes(out, "config").IsObject() {
		t.Fatalf("config must be object: %s", out)
	}
	if gjson.GetBytes(out, "params.model").String() != "deepseek/deepseek-v4-pro" {
		t.Fatalf("params.model=%s", gjson.GetBytes(out, "params.model").Raw)
	}
	if !gjson.GetBytes(out, "params.stream").Bool() {
		t.Fatal("params.stream want true")
	}
	if gjson.GetBytes(out, "threadId").String() == "" {
		t.Fatal("threadId required")
	}
	if gjson.GetBytes(out, "params.messages").Exists() {
		t.Fatal("envelope must not set params.messages")
	}
}
