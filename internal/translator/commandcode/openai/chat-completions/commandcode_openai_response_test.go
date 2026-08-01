package chat_completions

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertCommandCodeResponseToOpenAI_TextDeltaAndFinish(t *testing.T) {
	var param any
	ctx := context.Background()
	c1 := ConvertCommandCodeResponseToOpenAI(ctx, "m", nil, nil, []byte(`{"type":"text-delta","text":"Hello"}`), &param)
	if len(c1) == 0 || !bytes.Contains(c1[0], []byte(`"content":"Hello"`)) {
		t.Fatalf("chunk1=%s", c1)
	}
	c2 := ConvertCommandCodeResponseToOpenAI(ctx, "m", nil, nil, []byte(`{"type":"finish","finishReason":"stop","totalUsage":{"promptTokens":1,"completionTokens":2}}`), &param)
	joined := string(bytes.Join(c2, nil))
	if !strings.Contains(joined, `"finish_reason"`) {
		t.Fatalf("finish=%s", joined)
	}
}

func TestConvertCommandCodeResponseToOpenAI_ReasoningAndToolCall(t *testing.T) {
	var param any
	ctx := context.Background()
	c0 := ConvertCommandCodeResponseToOpenAI(ctx, "m", nil, nil, []byte(`{"type":"reasoning-delta","text":"think"}`), &param)
	if len(c0) == 0 || !bytes.Contains(c0[0], []byte(`"reasoning_content":"think"`)) {
		t.Fatalf("reasoning=%s", c0)
	}
	c1 := ConvertCommandCodeResponseToOpenAI(ctx, "m", nil, nil, []byte(`{"type":"tool-input-start","id":"t1","toolName":"ping"}`), &param)
	if len(c1) == 0 || !bytes.Contains(c1[0], []byte(`"tool_calls"`)) {
		t.Fatalf("tool-start=%s", c1)
	}
	c2 := ConvertCommandCodeResponseToOpenAI(ctx, "m", nil, nil, []byte(`{"type":"tool-input-delta","id":"t1","delta":"{}"}`), &param)
	if len(c2) == 0 || !bytes.Contains(c2[0], []byte(`"arguments":"{}"`)) {
		t.Fatalf("tool-delta=%s", c2)
	}
}

func TestConvertCommandCodeResponseToOpenAINonStream(t *testing.T) {
	body := []byte(`{"type":"text-delta","text":"Hi"}
{"type":"finish","finishReason":"stop","totalUsage":{"promptTokens":3,"completionTokens":4}}
`)
	out := ConvertCommandCodeResponseToOpenAINonStream(context.Background(), "m", nil, nil, body, nil)
	if gjson.GetBytes(out, "object").String() != "chat.completion" {
		t.Fatalf("object=%s", out)
	}
	if gjson.GetBytes(out, "choices.0.message.content").String() != "Hi" {
		t.Fatalf("content=%s", gjson.GetBytes(out, "choices.0.message.content").String())
	}
	if gjson.GetBytes(out, "choices.0.finish_reason").String() != "stop" {
		t.Fatalf("finish=%s", gjson.GetBytes(out, "choices.0.finish_reason").String())
	}
	if gjson.GetBytes(out, "usage.prompt_tokens").Int() != 3 {
		t.Fatalf("usage=%s", gjson.GetBytes(out, "usage").Raw)
	}
}

func TestConvertCommandCodeResponseToOpenAI_StreamToolCallsFinishReasonUpgraded(t *testing.T) {
	var param any
	ctx := context.Background()
	c1 := ConvertCommandCodeResponseToOpenAI(ctx, "m", nil, nil, []byte(`{"type":"tool-input-start","id":"t1","toolName":"foo"}`), &param)
	if len(c1) == 0 || !bytes.Contains(c1[0], []byte(`"tool_calls"`)) {
		t.Fatalf("tool-start=%s", c1)
	}
	c2 := ConvertCommandCodeResponseToOpenAI(ctx, "m", nil, nil, []byte(`{"type":"tool-input-delta","id":"t1","delta":"{}"}`), &param)
	if len(c2) == 0 || !bytes.Contains(c2[0], []byte(`"arguments":"{}"`)) {
		t.Fatalf("tool-delta=%s", c2)
	}
	c3 := ConvertCommandCodeResponseToOpenAI(ctx, "m", nil, nil, []byte(`{"type":"finish","finishReason":"stop"}`), &param)
	joined := string(bytes.Join(c3, nil))
	if !strings.Contains(joined, `"finish_reason":"tool_calls"`) {
		t.Fatalf("expected finish_reason tool_calls, got: %s", joined)
	}
}
