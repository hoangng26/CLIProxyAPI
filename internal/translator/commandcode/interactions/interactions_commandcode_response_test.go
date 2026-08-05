package interactions

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertCommandCodeResponseToInteractions_TextAndTerminal(t *testing.T) {
	var param any
	ctx := context.Background()
	c1 := ConvertCommandCodeResponseToInteractions(ctx, "m", nil, nil, []byte(`{"type":"text-delta","text":"Hello"}`), &param)
	if len(c1) == 0 {
		t.Fatal("expected interactions events for text")
	}
	c2 := ConvertCommandCodeResponseToInteractions(ctx, "m", nil, nil, []byte(`{"type":"finish","finishReason":"stop","totalUsage":{"promptTokens":1,"completionTokens":2}}`), &param)
	joined := string(bytes.Join(c2, nil))
	if !strings.Contains(joined, "interaction.completed") && !strings.Contains(joined, "completed") && !strings.Contains(joined, "[DONE]") {
		t.Fatalf("expected terminal interactions events: %s", joined)
	}
}

func TestConvertCommandCodeResponseToInteractionsNonStream(t *testing.T) {
	body := []byte(`{"type":"text-delta","text":"Hi"}
{"type":"finish","finishReason":"stop","totalUsage":{"promptTokens":3,"completionTokens":4}}
`)
	out := ConvertCommandCodeResponseToInteractionsNonStream(context.Background(), "m", nil, nil, body, nil)
	if !bytes.Contains(out, []byte("Hi")) {
		t.Fatalf("expected Hi: %s", out)
	}
	if gjson.GetBytes(out, "object").String() != "interaction" && gjson.GetBytes(out, "status").String() == "" {
		if !bytes.Contains(out, []byte("steps")) && !bytes.Contains(out, []byte("interaction")) {
			t.Fatalf("unexpected non-stream: %s", out)
		}
	}
}

func TestConvertCommandCodeResponseToInteractions_NoPanicOnBadParam(t *testing.T) {
	var param any = 123
	_ = ConvertCommandCodeResponseToInteractions(context.Background(), "m", nil, nil, []byte(`{"type":"text-delta","text":"x"}`), &param)
}
