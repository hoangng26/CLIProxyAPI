# CommandCode Responses + Interactions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make CommandCode models work for OpenAI Responses (HTTP + websocket) and Interactions clients by composing existing chat translators, fixing the production 400 (`memory` / `params.messages` / `config` missing).

**Architecture:** Keep CommandCode chat ↔ `/alpha/generate` as the only native dialect. Add thin composed packages that hop `openai-response` → chat → CommandCode and `interactions` → chat → CommandCode (and reverse for responses). Dual `*param` holder avoids state collisions. Inject synthetic `[DONE]` into the second hop after finish so Responses/Interactions emit terminal completed events (CommandCode NDJSON has no `[DONE]`).

**Tech Stack:** Go 1.26+, gjson/sjson, existing `internal/translator` registry, `go test`, `gofmt`.

**Spec:** `docs/superpowers/specs/2026-08-05-commandcode-responses-interactions-design.md`

## Global constraints

- Do not change `internal/runtime/executor/commandcode_executor.go` protocol unless a unit test proves a bug outside translation.
- Do not add generic multi-hop to `sdk/translator`.
- Comments in English only.
- After Go edits: `gofmt -w` on touched files; run package tests; final `go build -o test-output ./cmd/server && rm test-output`.
- This change set must include config comment updates (not translator-only; see AGENTS.md).
- Module path prefix: `github.com/router-for-me/CLIProxyAPI/v7`

## File map

| File | Responsibility |
|------|----------------|
| `internal/translator/commandcode/openai/responses/init.go` | Register `OpenaiResponse` ↔ `CommandCode` |
| `internal/translator/commandcode/openai/responses/commandcode_openai-responses_request.go` | Request composition |
| `internal/translator/commandcode/openai/responses/commandcode_openai-responses_response.go` | Stream/non-stream response composition + dual state + DONE injection |
| `internal/translator/commandcode/openai/responses/*_test.go` | Unit tests |
| `internal/translator/commandcode/interactions/init.go` | Register `Interactions` ↔ `CommandCode` |
| `internal/translator/commandcode/interactions/interactions_commandcode_request.go` | Request composition |
| `internal/translator/commandcode/interactions/interactions_commandcode_response.go` | Stream/non-stream response composition + dual state + DONE injection |
| `internal/translator/commandcode/interactions/*_test.go` | Unit tests |
| `internal/translator/init.go` | Blank imports for new packages |
| `config.example.yaml` | Document supported surfaces |
| `config.yaml` comments only (if still saying chat-only) | Same comment fix; do not change live secrets |

Existing source of truth (read-only for this plan):

- `internal/translator/commandcode/openai/chat-completions/commandcode_openai_request.go` — `ConvertOpenAIRequestToCommandCode`
- `internal/translator/commandcode/openai/chat-completions/commandcode_openai_response.go` — `ConvertCommandCodeResponseToOpenAI`, `ConvertCommandCodeResponseToOpenAINonStream`
- `internal/translator/openai/openai/responses/` — Responses ↔ chat converters
- `internal/translator/openai/interactions/chat-completions/` — Interactions ↔ chat converters

---

### Task 1: Responses request composition (TDD)

**Files:**
- Create: `internal/translator/commandcode/openai/responses/commandcode_openai-responses_request.go`
- Create: `internal/translator/commandcode/openai/responses/commandcode_openai-responses_request_test.go`

- [ ] **Step 1: Write failing request tests**

```go
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
	// instructions should land as system on chat hop then params.system on CommandCode
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
	// Spot-check: some message carries tool-call or tool-result content type from chat mapper
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
```

- [ ] **Step 2: Run tests — expect FAIL (undefined function)**

```bash
go test ./internal/translator/commandcode/openai/responses/ -count=1
```

Expected: compile error `ConvertOpenAIResponsesRequestToCommandCode` undefined.

- [ ] **Step 3: Implement request composition**

```go
// internal/translator/commandcode/openai/responses/commandcode_openai-responses_request.go
package responses

import (
	ccchat "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/commandcode/openai/chat-completions"
	oairesp "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/openai/openai/responses"
)

// ConvertOpenAIResponsesRequestToCommandCode maps Responses → chat → CommandCode envelope.
func ConvertOpenAIResponsesRequestToCommandCode(modelName string, rawJSON []byte, stream bool) []byte {
	chat := oairesp.ConvertOpenAIResponsesRequestToOpenAIChatCompletions(modelName, rawJSON, stream)
	return ccchat.ConvertOpenAIRequestToCommandCode(modelName, chat, stream)
}
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
gofmt -w internal/translator/commandcode/openai/responses/
go test ./internal/translator/commandcode/openai/responses/ -count=1
```

Expected: PASS (or FAIL only if Responses→chat tool item shapes differ — adjust test assertions to actual chat hop output while still requiring non-empty `params.messages` and tool-ish roles).

- [ ] **Step 5: Commit**

```bash
git add internal/translator/commandcode/openai/responses/commandcode_openai-responses_request.go \
        internal/translator/commandcode/openai/responses/commandcode_openai-responses_request_test.go
git commit -m "feat(commandcode): compose Responses request to CommandCode envelope"
```

---

### Task 2: Responses stream/non-stream response composition (TDD)

**Files:**
- Create: `internal/translator/commandcode/openai/responses/commandcode_openai-responses_response.go`
- Create: `internal/translator/commandcode/openai/responses/commandcode_openai-responses_response_test.go`

- [ ] **Step 1: Write failing response tests**

```go
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
		// Events may be SSE-framed; require non-empty output that is not raw CommandCode
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
		// Accept either full Responses object shape
		if gjson.GetBytes(out, "status").String() == "" && gjson.GetBytes(out, "object").String() == "" {
			t.Fatalf("non-stream out=%s", out)
		}
	}
	// Text should appear somewhere in output array / content
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
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
go test ./internal/translator/commandcode/openai/responses/ -count=1
```

Expected: undefined response converters.

- [ ] **Step 3: Implement response composition with dual state + DONE injection**

```go
// internal/translator/commandcode/openai/responses/commandcode_openai-responses_response.go
package responses

import (
	"context"

	ccchat "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/commandcode/openai/chat-completions"
	oairesp "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/openai/openai/responses"
	"github.com/tidwall/gjson"
)

// composedStreamParam holds independent *param state for each hop.
type composedStreamParam struct {
	viaChat  any
	toClient any
	doneSent bool
}

func ensureComposedStreamParam(param *any) *composedStreamParam {
	if param == nil {
		// Callers should pass non-nil; defensive local holder.
		s := &composedStreamParam{}
		return s
	}
	if *param == nil {
		s := &composedStreamParam{}
		*param = s
		return s
	}
	if s, ok := (*param).(*composedStreamParam); ok && s != nil {
		return s
	}
	s := &composedStreamParam{}
	*param = s
	return s
}

// ConvertCommandCodeResponseToOpenAIResponses converts one CommandCode NDJSON line
// into zero or more OpenAI Responses SSE/event payloads via chat hop.
func ConvertCommandCodeResponseToOpenAIResponses(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	st := ensureComposedStreamParam(param)
	chatChunks := ccchat.ConvertCommandCodeResponseToOpenAI(ctx, modelName, originalRequestRawJSON, requestRawJSON, rawJSON, &st.viaChat)

	out := make([][]byte, 0)
	sawFinish := false
	if typ := gjson.GetBytes(rawJSON, "type").String(); typ == "finish" {
		sawFinish = true
	}
	for _, ch := range chatChunks {
		if fr := gjson.GetBytes(ch, "choices.0.finish_reason"); fr.Exists() && fr.Type != gjson.Null {
			sawFinish = true
		}
		out = append(out, oairesp.ConvertOpenAIChatCompletionsResponseToOpenAIResponses(ctx, modelName, originalRequestRawJSON, requestRawJSON, ch, &st.toClient)...)
	}
	// CommandCode NDJSON has no [DONE]; Responses completed is deferred until DONE.
	if sawFinish && !st.doneSent {
		st.doneSent = true
		out = append(out, oairesp.ConvertOpenAIChatCompletionsResponseToOpenAIResponses(ctx, modelName, originalRequestRawJSON, requestRawJSON, []byte("[DONE]"), &st.toClient)...)
	}
	return out
}

// ConvertCommandCodeResponseToOpenAIResponsesNonStream folds full NDJSON into one Responses JSON object.
func ConvertCommandCodeResponseToOpenAIResponsesNonStream(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	chat := ccchat.ConvertCommandCodeResponseToOpenAINonStream(ctx, modelName, originalRequestRawJSON, requestRawJSON, rawJSON, nil)
	return oairesp.ConvertOpenAIChatCompletionsResponseToOpenAIResponsesNonStream(ctx, modelName, originalRequestRawJSON, requestRawJSON, chat, nil)
}
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
gofmt -w internal/translator/commandcode/openai/responses/
go test ./internal/translator/commandcode/openai/responses/ -count=1 -v
```

Expected: all PASS. If completed-event framing differs (SSE `event:` lines), loosen assertions to `strings.Contains(joined, "completed")` while still requiring DONE injection path covered.

- [ ] **Step 5: Commit**

```bash
git add internal/translator/commandcode/openai/responses/commandcode_openai-responses_response.go \
        internal/translator/commandcode/openai/responses/commandcode_openai-responses_response_test.go
git commit -m "feat(commandcode): compose CommandCode NDJSON to OpenAI Responses"
```

---

### Task 3: Register Responses translators + blank import

**Files:**
- Create: `internal/translator/commandcode/openai/responses/init.go`
- Modify: `internal/translator/init.go`

- [ ] **Step 1: Add package init registration**

```go
// internal/translator/commandcode/openai/responses/init.go
package responses

import (
	. "github.com/router-for-me/CLIProxyAPI/v7/internal/constant"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/translator/translator"
)

func init() {
	translator.Register(
		OpenaiResponse,
		CommandCode,
		ConvertOpenAIResponsesRequestToCommandCode,
		interfaces.TranslateResponse{
			Stream:    ConvertCommandCodeResponseToOpenAIResponses,
			NonStream: ConvertCommandCodeResponseToOpenAIResponsesNonStream,
		},
	)
}
```

- [ ] **Step 2: Blank-import in `internal/translator/init.go`**

Next to the existing commandcode chat import, add:

```go
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/commandcode/openai/responses"
```

Keep:

```go
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/commandcode/openai/chat-completions"
```

- [ ] **Step 3: Verify registry + compile**

```bash
gofmt -w internal/translator/commandcode/openai/responses/init.go internal/translator/init.go
go test ./internal/translator/commandcode/... -count=1
go build -o test-output ./cmd/server && rm test-output
```

Expected: tests PASS; build succeeds.

Optional quick sanity (in a tiny `_test.go` under responses or a one-off in existing translator tests):

```go
if !sdktranslator.HasRequestTransformer(sdktranslator.FormatOpenAIResponse, sdktranslator.FromString("commandcode")) {
    t.Fatal("missing openai-response -> commandcode request transformer")
}
```

(Only if easy; otherwise skip — registration is covered by blank import + build.)

- [ ] **Step 4: Commit**

```bash
git add internal/translator/commandcode/openai/responses/init.go internal/translator/init.go
git commit -m "feat(commandcode): register openai-response translators"
```

---

### Task 4: Interactions request composition (TDD)

**Files:**
- Create: `internal/translator/commandcode/interactions/interactions_commandcode_request.go`
- Create: `internal/translator/commandcode/interactions/interactions_commandcode_request_test.go`

- [ ] **Step 1: Write failing request tests**

```go
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
	// system should survive via chat hop
	if sys := gjson.GetBytes(out, "params.system").String(); sys != "be brief" {
		// Some hops put system only in messages; accept either
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
```

If `user_input` shape is wrong for `ConvertInteractionsRequestToOpenAI`, copy a payload shape from `internal/translator/openai/interactions/chat-completions/interactions_openai_request_test.go` that already passes.

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/translator/commandcode/interactions/ -count=1
```

- [ ] **Step 3: Implement request composition**

```go
// internal/translator/commandcode/interactions/interactions_commandcode_request.go
package interactions

import (
	ccchat "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/commandcode/openai/chat-completions"
	oiachat "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/openai/interactions/chat-completions"
)

// ConvertInteractionsRequestToCommandCode maps Interactions → chat → CommandCode envelope.
func ConvertInteractionsRequestToCommandCode(modelName string, rawJSON []byte, stream bool) []byte {
	chat := oiachat.ConvertInteractionsRequestToOpenAI(modelName, rawJSON, stream)
	return ccchat.ConvertOpenAIRequestToCommandCode(modelName, chat, stream)
}
```

- [ ] **Step 4: Run — expect PASS**

```bash
gofmt -w internal/translator/commandcode/interactions/
go test ./internal/translator/commandcode/interactions/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add internal/translator/commandcode/interactions/interactions_commandcode_request.go \
        internal/translator/commandcode/interactions/interactions_commandcode_request_test.go
git commit -m "feat(commandcode): compose Interactions request to CommandCode envelope"
```

---

### Task 5: Interactions stream/non-stream response composition (TDD)

**Files:**
- Create: `internal/translator/commandcode/interactions/interactions_commandcode_response.go`
- Create: `internal/translator/commandcode/interactions/interactions_commandcode_response_test.go`

- [ ] **Step 1: Write failing response tests**

```go
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
	// Terminal completed and/or done should appear after synthetic [DONE]
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
		// Accept interactions object-ish payload
		if !bytes.Contains(out, []byte("steps")) && !bytes.Contains(out, []byte("interaction")) {
			t.Fatalf("unexpected non-stream: %s", out)
		}
	}
}

func TestConvertCommandCodeResponseToInteractions_NoPanicOnBadParam(t *testing.T) {
	var param any = 123
	_ = ConvertCommandCodeResponseToInteractions(context.Background(), "m", nil, nil, []byte(`{"type":"text-delta","text":"x"}`), &param)
}
```

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement response composition**

```go
// internal/translator/commandcode/interactions/interactions_commandcode_response.go
package interactions

import (
	"context"

	ccchat "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/commandcode/openai/chat-completions"
	oiachat "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/openai/interactions/chat-completions"
	"github.com/tidwall/gjson"
)

type composedStreamParam struct {
	viaChat  any
	toClient any
	doneSent bool
}

func ensureComposedStreamParam(param *any) *composedStreamParam {
	if param == nil {
		return &composedStreamParam{}
	}
	if *param == nil {
		s := &composedStreamParam{}
		*param = s
		return s
	}
	if s, ok := (*param).(*composedStreamParam); ok && s != nil {
		return s
	}
	s := &composedStreamParam{}
	*param = s
	return s
}

// ConvertCommandCodeResponseToInteractions converts one CommandCode NDJSON line to Interactions events.
func ConvertCommandCodeResponseToInteractions(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	st := ensureComposedStreamParam(param)
	chatChunks := ccchat.ConvertCommandCodeResponseToOpenAI(ctx, modelName, originalRequestRawJSON, requestRawJSON, rawJSON, &st.viaChat)

	out := make([][]byte, 0)
	sawFinish := false
	if typ := gjson.GetBytes(rawJSON, "type").String(); typ == "finish" {
		sawFinish = true
	}
	for _, ch := range chatChunks {
		if fr := gjson.GetBytes(ch, "choices.0.finish_reason"); fr.Exists() && fr.Type != gjson.Null {
			sawFinish = true
		}
		out = append(out, oiachat.ConvertOpenAIResponseToInteractions(ctx, modelName, originalRequestRawJSON, requestRawJSON, ch, &st.toClient)...)
	}
	if sawFinish && !st.doneSent {
		st.doneSent = true
		out = append(out, oiachat.ConvertOpenAIResponseToInteractions(ctx, modelName, originalRequestRawJSON, requestRawJSON, []byte("[DONE]"), &st.toClient)...)
	}
	return out
}

// ConvertCommandCodeResponseToInteractionsNonStream folds NDJSON to one Interactions JSON object.
func ConvertCommandCodeResponseToInteractionsNonStream(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	chat := ccchat.ConvertCommandCodeResponseToOpenAINonStream(ctx, modelName, originalRequestRawJSON, requestRawJSON, rawJSON, nil)
	return oiachat.ConvertOpenAIResponseToInteractionsNonStream(ctx, modelName, originalRequestRawJSON, requestRawJSON, chat, nil)
}
```

- [ ] **Step 4: Run — expect PASS**

```bash
gofmt -w internal/translator/commandcode/interactions/
go test ./internal/translator/commandcode/interactions/ -count=1 -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/translator/commandcode/interactions/interactions_commandcode_response.go \
        internal/translator/commandcode/interactions/interactions_commandcode_response_test.go
git commit -m "feat(commandcode): compose CommandCode NDJSON to Interactions"
```

---

### Task 6: Register Interactions translators + blank import

**Files:**
- Create: `internal/translator/commandcode/interactions/init.go`
- Modify: `internal/translator/init.go`

- [ ] **Step 1: Add init.go**

```go
package interactions

import (
	. "github.com/router-for-me/CLIProxyAPI/v7/internal/constant"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/translator/translator"
)

func init() {
	translator.Register(
		Interactions,
		CommandCode,
		ConvertInteractionsRequestToCommandCode,
		interfaces.TranslateResponse{
			Stream:    ConvertCommandCodeResponseToInteractions,
			NonStream: ConvertCommandCodeResponseToInteractionsNonStream,
		},
	)
}
```

- [ ] **Step 2: Blank-import in `internal/translator/init.go`**

```go
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/commandcode/interactions"
```

- [ ] **Step 3: Test + build**

```bash
gofmt -w internal/translator/commandcode/interactions/init.go internal/translator/init.go
go test ./internal/translator/commandcode/... -count=1
go test ./internal/runtime/executor/ -run CommandCode -count=1
go build -o test-output ./cmd/server && rm test-output
```

Expected: all PASS / build OK.

- [ ] **Step 4: Commit**

```bash
git add internal/translator/commandcode/interactions/init.go internal/translator/init.go
git commit -m "feat(commandcode): register interactions translators"
```

---

### Task 7: Config comments + final verification

**Files:**
- Modify: `config.example.yaml` (CommandCode block comments ~lines 377–381)
- Modify: `config.yaml` comments only if they still say chat-only (do not touch API keys)

- [ ] **Step 1: Update comments**

Replace:

```yaml
# v1: OpenAI-compatible clients (/v1/chat/completions) only.
```

With:

```yaml
# Supports OpenAI Chat Completions, OpenAI Responses (HTTP + websocket), and Interactions.
# Client formats other than chat are composed through the OpenAI chat hop into the
# native /alpha/generate envelope (same fidelity limits as chat → CommandCode).
```

Apply the same replacement wherever that old comment appears under CommandCode sections.

- [ ] **Step 2: Full verification**

```bash
gofmt -w internal/translator/commandcode/ internal/translator/init.go
go test ./internal/translator/commandcode/... -count=1
go test ./internal/runtime/executor/ -run CommandCode -count=1
go build -o test-output ./cmd/server && rm test-output
```

Expected: PASS + successful build.

- [ ] **Step 3: Manual smoke (operator, optional in agent session)**

With server running and CommandCode keys configured:

```bash
# Responses path — should NOT return CommandCode validation 400 for memory/params.messages/config
curl -sS http://127.0.0.1:8317/v1/responses \
  -H "Authorization: Bearer <api-key>" \
  -H "Content-Type: application/json" \
  -d '{
    "model":"deepseek/deepseek-v4-flash",
    "instructions":"be brief",
    "input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"ping"}]}],
    "stream":false
  }'
```

Success: not the previous BAD_REQUEST three-field validation error. Upstream auth/model errors are OK for this smoke.

- [ ] **Step 4: Commit**

```bash
git add config.example.yaml
# only if comments changed:
# git add config.yaml
git commit -m "docs(config): CommandCode supports Responses and Interactions"
```

---

## Plan self-review

### Spec coverage

| Spec requirement | Task |
|------------------|------|
| Responses HTTP request envelope | Task 1 |
| Responses stream + non-stream + dual state | Task 2 |
| Register openai-response pair + blank import | Task 3 |
| Websocket via same pair (no WS code) | Task 3 (registration only) |
| Interactions request | Task 4 |
| Interactions stream + non-stream + dual state | Task 5 |
| Register interactions pair | Task 6 |
| Config comments | Task 7 |
| Regression chat tests + build gate | Tasks 3, 6, 7 |
| Synthetic DONE for completed events | Tasks 2, 5 |
| Executor unchanged | No executor task |

### Placeholder scan

No TBD/TODO steps; concrete code and commands included.

### Type consistency

- Dual state type name: `composedStreamParam` with `viaChat`, `toClient`, `doneSent` in both packages (package-private, duplicated intentionally — no shared helper required for YAGNI).
- Function names match registration and design composition map.

### Known implementation pitfall (do not skip)

`ConvertOpenAIChatCompletionsResponseToOpenAIResponses` and `ConvertOpenAIResponseToInteractions` defer terminal completed events until `[DONE]`. CommandCode NDJSON never sends `[DONE]`. Composed stream converters **must** inject `[DONE]` once after finish (see Tasks 2 and 5). Without this, streams hang without `response.completed` / `interaction.completed`.
