package responses

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIResponsesRequestToCommandCode_BasicEnvelope(t *testing.T) {
	in := []byte(`{
	  "model":"deepseek/deepseek-v4-flash",
	  "instructions":"be brief",
	  "input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}],
	  "stream":true
	}`)
	out := ConvertOpenAIResponsesRequestToCommandCode("deepseek/deepseek-v4-flash", in, true)

	if gjson.GetBytes(out, "memory").Type != gjson.String {
		t.Fatalf("memory must be string, got %s", gjson.GetBytes(out, "memory").Raw)
	}
	if !gjson.GetBytes(out, "config").IsObject() {
		t.Fatalf("config must be object, got %s", gjson.GetBytes(out, "config").Raw)
	}
	msgs := gjson.GetBytes(out, "params.messages")
	if !msgs.IsArray() || len(msgs.Array()) == 0 {
		t.Fatalf("params.messages missing: %s", out)
	}
	if gjson.GetBytes(out, "params.model").String() != "deepseek/deepseek-v4-flash" {
		t.Fatalf("params.model=%s", gjson.GetBytes(out, "params.model").Raw)
	}
	if !gjson.GetBytes(out, "params.stream").Bool() {
		t.Fatal("params.stream want true")
	}
	if gjson.GetBytes(out, "params.system").String() != "be brief" {
		t.Fatalf("params.system=%q out=%s", gjson.GetBytes(out, "params.system").String(), out)
	}
}

func TestConvertOpenAIResponsesRequestToCommandCode_ToolRoundTrip(t *testing.T) {
	in := []byte(`{
	  "model":"m",
	  "input":[
	    {"type":"function_call","call_id":"c1","name":"ping","arguments":"{\"x\":1}"},
	    {"type":"function_call_output","call_id":"c1","output":"pong"}
	  ]
	}`)
	out := ConvertOpenAIResponsesRequestToCommandCode("m", in, false)
	msgs := gjson.GetBytes(out, "params.messages").Array()
	if len(msgs) < 2 {
		t.Fatalf("want tool round-trip messages, got %s", gjson.GetBytes(out, "params.messages").Raw)
	}
	joined := gjson.GetBytes(out, "params.messages").Raw
	if !gjson.Valid(joined) {
		t.Fatalf("invalid messages json: %s", joined)
	}
	foundToolish := false
	for _, m := range msgs {
		role := m.Get("role").String()
		if role == "assistant" || role == "tool" {
			foundToolish = true
		}
		m.Get("content").ForEach(func(_, c gjson.Result) bool {
			typ := c.Get("type").String()
			if typ == "tool-call" || typ == "tool-result" {
				foundToolish = true
			}
			return true
		})
	}
	if !foundToolish {
		t.Fatalf("expected tool-ish messages after composition: %s", joined)
	}
}
