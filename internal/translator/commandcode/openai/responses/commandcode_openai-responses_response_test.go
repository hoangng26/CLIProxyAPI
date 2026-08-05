package responses

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertCommandCodeResponseToOpenAIResponses_TextAndCompleted(t *testing.T) {
	var param any
	ctx := context.Background()
	orig := []byte(`{"model":"m","instructions":"x"}`)

	c1 := ConvertCommandCodeResponseToOpenAIResponses(ctx, "m", orig, orig, []byte(`{"type":"text-delta","text":"Hello"}`), &param)
	if len(c1) == 0 {
		t.Fatal("expected some Responses events for text-delta")
	}
	joined1 := string(bytes.Join(c1, nil))
	if !strings.Contains(joined1, "Hello") && !strings.Contains(joined1, "response.") {
		t.Fatalf("unexpected text events: %s", joined1)
	}

	c2 := ConvertCommandCodeResponseToOpenAIResponses(ctx, "m", orig, orig, []byte(`{"type":"finish","finishReason":"stop","totalUsage":{"promptTokens":1,"completionTokens":2}}`), &param)
	joined2 := string(bytes.Join(c2, nil))
	if !strings.Contains(joined2, "response.completed") && !strings.Contains(joined2, `"status":"completed"`) {
		t.Fatalf("expected completed after finish+[DONE] injection, got: %s", joined2)
	}
}

func TestConvertCommandCodeResponseToOpenAIResponses_ToolCall(t *testing.T) {
	var param any
	ctx := context.Background()
	_ = ConvertCommandCodeResponseToOpenAIResponses(ctx, "m", nil, nil, []byte(`{"type":"tool-input-start","id":"t1","toolName":"ping"}`), &param)
	_ = ConvertCommandCodeResponseToOpenAIResponses(ctx, "m", nil, nil, []byte(`{"type":"tool-input-delta","id":"t1","delta":"{}"}`), &param)
	out := ConvertCommandCodeResponseToOpenAIResponses(ctx, "m", nil, nil, []byte(`{"type":"finish","finishReason":"stop"}`), &param)
	joined := string(bytes.Join(out, nil))
	if !strings.Contains(joined, "function_call") && !strings.Contains(joined, "ping") {
		t.Fatalf("expected function call in Responses stream: %s", joined)
	}
}

func TestConvertCommandCodeResponseToOpenAIResponsesNonStream(t *testing.T) {
	body := []byte(`{"type":"text-delta","text":"Hi"}
{"type":"finish","finishReason":"stop","totalUsage":{"promptTokens":3,"completionTokens":4}}
`)
	out := ConvertCommandCodeResponseToOpenAIResponsesNonStream(context.Background(), "m", nil, nil, body, nil)
	if gjson.GetBytes(out, "object").String() != "response" && gjson.GetBytes(out, "status").String() != "completed" {
		if gjson.GetBytes(out, "status").String() == "" && gjson.GetBytes(out, "object").String() == "" {
			t.Fatalf("non-stream out=%s", out)
		}
	}
	if !bytes.Contains(out, []byte("Hi")) {
		t.Fatalf("expected Hi in non-stream Responses: %s", out)
	}
}

func TestConvertCommandCodeResponseToOpenAIResponses_DualStateSurvivesWrongParam(t *testing.T) {
	var param any = "wrong-type"
	ctx := context.Background()
	// Must not panic
	_ = ConvertCommandCodeResponseToOpenAIResponses(ctx, "m", nil, nil, []byte(`{"type":"text-delta","text":"x"}`), &param)
}
