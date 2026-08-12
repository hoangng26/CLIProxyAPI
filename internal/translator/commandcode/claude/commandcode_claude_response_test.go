package claude

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func collectTypes(frames [][]byte) []string {
	var types []string
	for _, f := range frames {
		for _, line := range bytes.Split(f, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			payload := bytes.TrimSpace(line[5:])
			if typ := gjson.GetBytes(payload, "type").String(); typ != "" {
				types = append(types, typ)
			}
		}
	}
	return types
}

func TestConvertCommandCodeResponseToClaude_TextThenFinish(t *testing.T) {
	var param any
	ctx := context.Background()
	start := ConvertCommandCodeResponseToClaude(ctx, "m", nil, nil, []byte(`{"type":"text-delta","text":"Hi"}`), &param)
	end := ConvertCommandCodeResponseToClaude(ctx, "m", nil, nil, []byte(`{"type":"finish","finishReason":"stop"}`), &param)
	types := append(collectTypes(start), collectTypes(end)...)
	want := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("types=%v want=%v", types, want)
	}
	joined := string(bytes.Join(append(start, end...), nil))
	if !strings.Contains(joined, `"text":"Hi"`) && !strings.Contains(joined, `"text": "Hi"`) {
		t.Fatalf("missing text delta: %s", joined)
	}
	if !strings.Contains(joined, `"stop_reason":"end_turn"`) && !strings.Contains(joined, `"stop_reason": "end_turn"`) {
		t.Fatalf("stop_reason: %s", joined)
	}
}

func TestConvertCommandCodeResponseToClaude_ReasoningThenText(t *testing.T) {
	var param any
	ctx := context.Background()
	a := ConvertCommandCodeResponseToClaude(ctx, "m", nil, nil, []byte(`{"type":"reasoning-delta","text":"think"}`), &param)
	b := ConvertCommandCodeResponseToClaude(ctx, "m", nil, nil, []byte(`{"type":"text-delta","text":"hi"}`), &param)
	types := append(collectTypes(a), collectTypes(b)...)
	// thinking start/delta, stop, text start/delta
	joined := strings.Join(types, ",")
	if !strings.Contains(joined, "message_start") {
		t.Fatalf("missing message_start: %v", types)
	}
	if strings.Count(joined, "content_block_start") < 2 {
		t.Fatalf("want thinking then text starts: %v", types)
	}
	if strings.Count(joined, "content_block_stop") < 1 {
		t.Fatalf("need stop between blocks: %v", types)
	}
	raw := string(bytes.Join(append(a, b...), nil))
	if !strings.Contains(raw, "thinking") {
		t.Fatalf("missing thinking: %s", raw)
	}
}

func TestConvertCommandCodeResponseToClaude_ToolInput(t *testing.T) {
	var param any
	ctx := context.Background()
	frames := [][]byte{}
	for _, line := range []string{
		`{"type":"tool-input-start","id":"c1","toolName":"ping"}`,
		`{"type":"tool-input-delta","id":"c1","delta":"{\"x\":1}"}`,
		`{"type":"finish","finishReason":"tool-use"}`,
	} {
		frames = append(frames, ConvertCommandCodeResponseToClaude(ctx, "m", nil, nil, []byte(line), &param)...)
	}
	raw := string(bytes.Join(frames, nil))
	if !strings.Contains(raw, `"type":"tool_use"`) && !strings.Contains(raw, `"type": "tool_use"`) {
		t.Fatalf("missing tool_use: %s", raw)
	}
	if !strings.Contains(raw, `"stop_reason":"tool_use"`) && !strings.Contains(raw, `"stop_reason": "tool_use"`) {
		t.Fatalf("stop_reason: %s", raw)
	}
}

func TestConvertCommandCodeResponseToClaude_SkipsBadLine(t *testing.T) {
	var param any
	got := ConvertCommandCodeResponseToClaude(context.Background(), "m", nil, nil, []byte(`not-json`), &param)
	if len(got) != 0 {
		t.Fatalf("bad line must skip, got %q", got)
	}
}

func TestConvertCommandCodeResponseToClaudeNonStream_TextAndUsage(t *testing.T) {
	body := []byte("{\"type\":\"text-delta\",\"text\":\"Hi\"}\n{\"type\":\"finish\",\"finishReason\":\"stop\",\"totalUsage\":{\"promptTokens\":3,\"completionTokens\":2}}\n")
	out := ConvertCommandCodeResponseToClaudeNonStream(context.Background(), "m", nil, nil, body, nil)
	if gjson.GetBytes(out, "type").String() != "message" {
		t.Fatalf("type=%s out=%s", gjson.GetBytes(out, "type").String(), out)
	}
	if gjson.GetBytes(out, "role").String() != "assistant" {
		t.Fatal("role")
	}
	if gjson.GetBytes(out, "content.0.type").String() != "text" || gjson.GetBytes(out, "content.0.text").String() != "Hi" {
		t.Fatalf("content=%s", gjson.GetBytes(out, "content").Raw)
	}
	if gjson.GetBytes(out, "stop_reason").String() != "end_turn" {
		t.Fatalf("stop_reason=%s", gjson.GetBytes(out, "stop_reason").Raw)
	}
	if gjson.GetBytes(out, "usage.input_tokens").Int() != 3 || gjson.GetBytes(out, "usage.output_tokens").Int() != 2 {
		t.Fatalf("usage=%s", gjson.GetBytes(out, "usage").Raw)
	}
}
