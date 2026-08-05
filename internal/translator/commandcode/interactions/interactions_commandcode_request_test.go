package interactions

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertInteractionsRequestToCommandCode_BasicEnvelope(t *testing.T) {
	in := []byte(`{
	  "model":"deepseek/deepseek-v4-flash",
	  "system_instruction":"be brief",
	  "input":[{"type":"user_input","content":[{"type":"text","text":"hi"}]}],
	  "stream":true
	}`)
	out := ConvertInteractionsRequestToCommandCode("deepseek/deepseek-v4-flash", in, true)

	if gjson.GetBytes(out, "memory").Type != gjson.String {
		t.Fatalf("memory must be string: %s", out)
	}
	if !gjson.GetBytes(out, "config").IsObject() {
		t.Fatalf("config must be object: %s", out)
	}
	msgs := gjson.GetBytes(out, "params.messages")
	if !msgs.IsArray() || len(msgs.Array()) == 0 {
		t.Fatalf("params.messages missing: %s", out)
	}
	if gjson.GetBytes(out, "params.model").String() != "deepseek/deepseek-v4-flash" {
		t.Fatalf("params.model=%s", gjson.GetBytes(out, "params.model").Raw)
	}
	if sys := gjson.GetBytes(out, "params.system").String(); sys != "be brief" {
		if !containsRole(msgs, "user") {
			t.Fatalf("system=%q messages=%s", sys, msgs.Raw)
		}
	}
}

func containsRole(msgs gjson.Result, role string) bool {
	ok := false
	msgs.ForEach(func(_, m gjson.Result) bool {
		if m.Get("role").String() == role {
			ok = true
			return false
		}
		return true
	})
	return ok
}
