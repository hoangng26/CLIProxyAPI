package responses

import (
	"testing"

	"github.com/tidwall/gjson"
)

// Codex Responses history often interleaves assistant text among function_call
// items that still belong to one turn (outputs arrive after all calls).
// Providers like CommandCode/AI SDK require every tool_call_id from an
// assistant tool_calls message to be answered by the immediately following
// tool messages.
func TestConvertOpenAIResponsesRequestToOpenAIChatCompletions_InterleavedAssistantKeepsToolCallsTogether(t *testing.T) {
	raw := []byte(`{
		"input": [
			{"type":"function_call","call_id":"c0","name":"update_plan","arguments":"{}"},
			{"type":"function_call","call_id":"c1","name":"exec_command","arguments":"{\"cmd\":\"ls\"}"},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"continuing tools"}]},
			{"type":"function_call","call_id":"c2","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call_output","call_id":"c0","output":"ok0"},
			{"type":"function_call_output","call_id":"c1","output":"ok1"},
			{"type":"function_call_output","call_id":"c2","output":"ok2"}
		]
	}`)

	out := ConvertOpenAIResponsesRequestToOpenAIChatCompletions("deepseek/deepseek-v4-pro", raw, true)
	t.Logf("output json:\n%s", prettyJSONForTest(out))

	msgs := gjson.GetBytes(out, "messages").Array()
	if len(msgs) != 5 {
		t.Fatalf("messages count = %d, want 5; output=%s", len(msgs), out)
	}

	// assistant commentary first (emitted while tool calls stay buffered)
	if got := msgs[0].Get("role").String(); got != "assistant" {
		t.Fatalf("messages.0.role = %q, want assistant", got)
	}
	if msgs[0].Get("tool_calls").Exists() {
		t.Fatalf("messages.0 should not have tool_calls yet; output=%s", out)
	}

	// one assistant tool_calls block with all three ids
	if got := msgs[1].Get("role").String(); got != "assistant" {
		t.Fatalf("messages.1.role = %q, want assistant", got)
	}
	toolCalls := msgs[1].Get("tool_calls").Array()
	if len(toolCalls) != 3 {
		t.Fatalf("messages.1.tool_calls length = %d, want 3; output=%s", len(toolCalls), out)
	}
	for i, want := range []string{"c0", "c1", "c2"} {
		if got := toolCalls[i].Get("id").String(); got != want {
			t.Fatalf("messages.1.tool_calls.%d.id = %q, want %q", i, got, want)
		}
	}

	for i, want := range []string{"c0", "c1", "c2"} {
		idx := i + 2
		if got := msgs[idx].Get("role").String(); got != "tool" {
			t.Fatalf("messages.%d.role = %q, want tool", idx, got)
		}
		if got := msgs[idx].Get("tool_call_id").String(); got != want {
			t.Fatalf("messages.%d.tool_call_id = %q, want %q", idx, got, want)
		}
	}
}
