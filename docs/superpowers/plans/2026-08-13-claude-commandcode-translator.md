# Claude ↔ CommandCode Translator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Claude `/v1/messages` to CommandCode models (`deepseek/deepseek-v4-pro` and the rest of that catalog) emit a valid `/alpha/generate` envelope and stream Claude SSE back, without an OpenAI hop.

**Architecture:** Export `BuildCommandCodeEnvelope` and `ConvertTools` from the existing chat-completions CommandCode package. Add `internal/translator/commandcode/claude/` with a dedicated Claude request mapper and a dedicated NDJSON → Claude SSE state machine. Blank-import the package from `internal/translator/init.go`. Leave `CommandCodeExecutor` unchanged.

**Tech Stack:** Go 1.26+, gjson/sjson, `internal/translator` registry, `internal/translator/common.AppendSSEEventBytes`, `go test`, `gofmt`.

**Spec:** `docs/superpowers/specs/2026-08-13-claude-commandcode-translator-design.md`

## Global Constraints

- Comments in English only.
- Do not change `internal/runtime/executor/commandcode_executor.go` protocol, headers, or path.
- Do not add generic multi-hop to `sdk/translator`.
- Do not call `ConvertCommandCodeResponseToOpenAI` or `ConvertOpenAIResponseToClaude`.
- Do not implement Gemini → CommandCode.
- Do not implement `CountTokens` for CommandCode.
- Drop prior-turn `thinking` / `redacted_thinking` and image parts.
- Drop `cache_control`, `tool_choice`, top-level `thinking`, `output_config`, `context_management`.
- After Go edits: `gofmt -w` on touched files.
- Module path prefix: `github.com/router-for-me/CLIProxyAPI/v7`
- AGENTS.md: this is a standalone `internal/translator/` change. Task 1 must confirm `WRITE` / `MAINTAIN` / `ADMIN` or file an issue and stop.

## File map

| File | Responsibility |
|------|----------------|
| `internal/translator/commandcode/openai/chat-completions/commandcode_openai_request.go` | Export `BuildCommandCodeEnvelope` and `ConvertTools`; OpenAI converter calls them |
| `internal/translator/commandcode/openai/chat-completions/commandcode_openai_request_test.go` | Existing tests must stay green; add envelope helper test |
| `internal/translator/commandcode/claude/init.go` | Register `Claude` ↔ `CommandCode` |
| `internal/translator/commandcode/claude/commandcode_claude_request.go` | Claude → envelope |
| `internal/translator/commandcode/claude/commandcode_claude_request_test.go` | Request unit tests |
| `internal/translator/commandcode/claude/commandcode_claude_response.go` | NDJSON → Claude SSE + non-stream fold |
| `internal/translator/commandcode/claude/commandcode_claude_response_test.go` | Response unit tests |
| `internal/translator/init.go` | Blank-import `commandcode/claude` |

Do not touch executor, registry models, or auth.

### Exported signatures (locked)

```go
// package chat_completions
func BuildCommandCodeEnvelope(modelName string, stream bool) []byte
func ConvertTools(tools gjson.Result) []byte

// package claude
func ConvertClaudeRequestToCommandCode(modelName string, rawJSON []byte, stream bool) []byte
func ConvertCommandCodeResponseToClaude(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte
func ConvertCommandCodeResponseToClaudeNonStream(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []byte
```

`ConvertTools` is the existing unexported `convertTools` renamed (or a one-line wrapper that calls it). Behavior must not change.

---

### Task 1: Permission gate

**Files:** none

**Interfaces:**
- Consumes: AGENTS.md translator-only rule
- Produces: go / no-go for every later task

- [ ] **Step 1: Check write permission**

```bash
gh repo view --json viewerPermission -q .viewerPermission
```

Expected: `WRITE`, `MAINTAIN`, or `ADMIN`.

If the value is anything else (including empty / error):

1. Open a GitHub issue titled `feat(commandcode): Claude /v1/messages translator`.
2. Body must include: the 400 (`memory` / `params.messages` / `config` undefined), the spec path, and that the intended fix is `internal/translator/commandcode/claude/` plus exported `BuildCommandCodeEnvelope` / `ConvertTools`.
3. **Stop. Do not edit Go files.**

- [ ] **Step 2: Commit nothing**

Permission check only. No commit.

---

### Task 2: Shared envelope + ConvertTools

**Files:**
- Modify: `internal/translator/commandcode/openai/chat-completions/commandcode_openai_request.go`
- Modify: `internal/translator/commandcode/openai/chat-completions/commandcode_openai_request_test.go`

**Interfaces:**
- Consumes: current `ConvertOpenAIRequestToCommandCode` body (lines 20–68)
- Produces: `BuildCommandCodeEnvelope(modelName string, stream bool) []byte`, `ConvertTools(tools gjson.Result) []byte`

- [ ] **Step 1: Write the failing helper test**

Append to `commandcode_openai_request_test.go`:

```go
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
```

- [ ] **Step 2: Run the test and confirm it fails**

```bash
go test -count=1 -run TestBuildCommandCodeEnvelope_ZodShape ./internal/translator/commandcode/openai/chat-completions
```

Expected: FAIL, `BuildCommandCodeEnvelope` undefined.

- [ ] **Step 3: Extract the helper and switch the OpenAI converter**

In `commandcode_openai_request.go`, add:

```go
// BuildCommandCodeEnvelope returns the /alpha/generate skeleton.
func BuildCommandCodeEnvelope(modelName string, stream bool) []byte {
	out := []byte(`{"threadId":"","memory":"","config":{},"params":{}}`)
	out, _ = sjson.SetBytes(out, "threadId", uuid.NewString())
	out, _ = sjson.SetBytes(out, "memory", "")
	out, _ = sjson.SetBytes(out, "config.workingDir", "")
	out, _ = sjson.SetBytes(out, "config.date", time.Now().UTC().Format("2006-01-02"))
	out, _ = sjson.SetBytes(out, "config.environment", runtime.GOOS)
	out, _ = sjson.SetRawBytes(out, "config.structure", []byte("[]"))
	out, _ = sjson.SetBytes(out, "config.isGitRepo", false)
	out, _ = sjson.SetBytes(out, "config.currentBranch", "")
	out, _ = sjson.SetBytes(out, "config.mainBranch", "")
	out, _ = sjson.SetBytes(out, "config.gitStatus", "")
	out, _ = sjson.SetRawBytes(out, "config.recentCommits", []byte("[]"))
	out, _ = sjson.SetBytes(out, "params.model", modelName)
	out, _ = sjson.SetBytes(out, "params.stream", stream)
	return out
}

// ConvertTools maps OpenAI or Anthropic tool lists to CommandCode params.tools.
func ConvertTools(tools gjson.Result) []byte {
	return convertTools(tools)
}
```

Replace the start of `ConvertOpenAIRequestToCommandCode` so it begins with `out := BuildCommandCodeEnvelope(modelName, stream)` instead of rebuilding the skeleton. Replace `convertTools(root.Get("tools"))` with `ConvertTools(root.Get("tools"))`. Leave `convertTools` in place as the implementation.

- [ ] **Step 4: Run helper + existing OpenAI tests**

```bash
gofmt -w internal/translator/commandcode/openai/chat-completions/commandcode_openai_request.go internal/translator/commandcode/openai/chat-completions/commandcode_openai_request_test.go
go test -count=1 ./internal/translator/commandcode/openai/chat-completions ./internal/translator/commandcode/openai/responses ./internal/translator/commandcode/interactions
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/translator/commandcode/openai/chat-completions/commandcode_openai_request.go internal/translator/commandcode/openai/chat-completions/commandcode_openai_request_test.go
git commit -m "refactor(commandcode): export envelope builder and ConvertTools"
```

---

### Task 3: Claude request mapper

**Files:**
- Create: `internal/translator/commandcode/claude/commandcode_claude_request.go`
- Create: `internal/translator/commandcode/claude/commandcode_claude_request_test.go`

**Interfaces:**
- Consumes: `chat_completions.BuildCommandCodeEnvelope`, `chat_completions.ConvertTools`
- Produces: `ConvertClaudeRequestToCommandCode(modelName string, rawJSON []byte, stream bool) []byte`

- [ ] **Step 1: Write failing request tests**

Create `commandcode_claude_request_test.go`:

```go
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
```

- [ ] **Step 2: Run tests and confirm they fail**

```bash
go test -count=1 ./internal/translator/commandcode/claude
```

Expected: FAIL, package or `ConvertClaudeRequestToCommandCode` undefined.

- [ ] **Step 3: Implement the request mapper**

Create `commandcode_claude_request.go` in package `claude`.

Rules (must match tests):

1. `out := ccchat.BuildCommandCodeEnvelope(modelName, stream)`.
2. If `rawJSON` is not valid JSON, set `params.messages` to `[]` and return.
3. `params.max_tokens` from `max_tokens` if present, else `32000`.
4. `params.temperature` from `temperature` if present, else `0.3`.
5. `params.top_p` only if present.
6. `system`: if string, use it; if array, collect `text` fields, join with `"\n\n"`. Ignore `cache_control`.
7. Walk `messages` in order:
   - Build `toolNameByCallID` from assistant `tool_use` blocks (`id` → `name`).
   - Skip `thinking` and `redacted_thinking`.
   - Skip `image`, `image_url`, and blocks with `source.data` / `source.base64`.
   - User `text` (string content or `{type:text}`) → one `{role:user, content:[{type:text,text}]}` message. Multiple text parts in one Claude message become multiple text blocks in that user message.
   - User `tool_result` → `{role:tool, content:[{type:tool-result, toolCallId, toolName, output:{type:text, value}}]}`. `toolCallId` from `tool_use_id` or `tool_call_id`. `toolName` from the block `name` or `toolNameByCallID`. `value` from string `content` or concatenated text parts.
   - Assistant text + `tool_use` → one `{role:assistant, content:[...]}`. `tool_use` becomes `{type:tool-call, toolCallId, toolName, input}` where `input` is the object (or `{}`).
   - If a user message has no remaining parts after drops, skip it.
8. If the input was valid JSON, `messages` existed, and every user/assistant message was skipped, append one `{role:user, content:[{type:text,text:""}]}`.
9. `tools` via `ccchat.ConvertTools(root.Get("tools"))` when non-empty.
10. Do not copy `tool_choice`, top-level `thinking`, `output_config`, or `context_management`.

Import:

```go
ccchat "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/commandcode/openai/chat-completions"
```

- [ ] **Step 4: Run request tests**

```bash
gofmt -w internal/translator/commandcode/claude/commandcode_claude_request.go internal/translator/commandcode/claude/commandcode_claude_request_test.go
go test -count=1 ./internal/translator/commandcode/claude
```

Expected: PASS (response files may still be absent; request tests only).

- [ ] **Step 5: Commit**

```bash
git add internal/translator/commandcode/claude/commandcode_claude_request.go internal/translator/commandcode/claude/commandcode_claude_request_test.go
git commit -m "feat(commandcode): map Claude /v1/messages into CommandCode envelope"
```

---

### Task 4: Claude stream response mapper

**Files:**
- Create: `internal/translator/commandcode/claude/commandcode_claude_response.go`
- Create: `internal/translator/commandcode/claude/commandcode_claude_response_test.go`

**Interfaces:**
- Consumes: CommandCode NDJSON event types already handled in `commandcode_openai_response.go` (`text-delta`, `reasoning-delta`, `tool-input-start`, `tool-input-delta`, `tool-call`, `finish-step`, `finish`)
- Produces: `ConvertCommandCodeResponseToClaude(...)` returning Claude SSE frames via `translatorcommon.AppendSSEEventBytes`

- [ ] **Step 1: Write failing stream tests**

Create `commandcode_claude_response_test.go`:

```go
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
```

- [ ] **Step 2: Run stream tests and confirm they fail**

```bash
go test -count=1 -run TestConvertCommandCodeResponseToClaude_ ./internal/translator/commandcode/claude
```

Expected: FAIL, `ConvertCommandCodeResponseToClaude` undefined.

- [ ] **Step 3: Implement the stream state machine**

Create `commandcode_claude_response.go`.

State (store in `*param`):

```go
type claudeStreamState struct {
	MessageID         string
	Model             string
	MessageStarted    bool
	MessageStopped    bool
	OpenBlock         string // "", "thinking", "text", "tool_use"
	OpenIndex         int
	NextIndex         int
	SawToolUse        bool
	ToolIndexByID     map[string]int
	InputTokens       int64
	OutputTokens      int64
	HasUsage          bool
	PendingStopReason string
}
```

Emit SSE with `translatorcommon.AppendSSEEventBytes(nil, event, payload, 2)`.

Behavior:

- Empty / non-object / unknown `type`: return nil. Do not start a message.
- First recognized event: emit `message_start` once:

```json
{"type":"message_start","message":{"id":"<msg_...>","type":"message","role":"assistant","content":[],"model":"<model>","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}
```

- `reasoning-delta`: if open block is not thinking, stop it; start thinking block `{"type":"thinking","thinking":""}`; emit `thinking_delta` with `text` or `delta`.
- `text-delta`: same pattern for text block `{"type":"text","text":""}` and `text_delta`.
- `tool-input-start`: stop open block; start `tool_use` `{id, name, input:{}}`; record id → index.
- `tool-input-delta`: `input_json_delta` / `partial_json` using `delta` or `inputTextDelta`. Unknown id: ignore.
- `tool-call`: if id already started, ignore. Else start + one `input_json_delta` from `input` + stop.
- `finish-step`: record `finishReason` (map `tool-use` / `tool_calls` / `tool-calls` → `tool_use`; `length` / `max_tokens` → `max_tokens`; else `end_turn`) and usage. Do not emit `message_stop` yet.
- `finish`: merge usage (`totalUsage` or `usage`, same field aliases as the OpenAI hop: `promptTokens` / `prompt_tokens` / `inputTokens` / `input_tokens` and completion counterparts). Stop open block. If any tool_use was emitted and stop reason is `end_turn`, use `tool_use`. Emit `message_delta` then `message_stop`. Set `MessageStopped`.
- `error`: treat as text `"\n\n[CommandCode error: <msg>]"` then finish with `end_turn`.

Do not call the OpenAI converters.

- [ ] **Step 4: Run stream tests**

```bash
gofmt -w internal/translator/commandcode/claude/commandcode_claude_response.go internal/translator/commandcode/claude/commandcode_claude_response_test.go
go test -count=1 ./internal/translator/commandcode/claude
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/translator/commandcode/claude/commandcode_claude_response.go internal/translator/commandcode/claude/commandcode_claude_response_test.go
git commit -m "feat(commandcode): stream CommandCode NDJSON as Claude SSE"
```

---

### Task 5: Non-stream fold + register + blank import

**Files:**
- Modify: `internal/translator/commandcode/claude/commandcode_claude_response.go` (add `ConvertCommandCodeResponseToClaudeNonStream`)
- Modify: `internal/translator/commandcode/claude/commandcode_claude_response_test.go`
- Create: `internal/translator/commandcode/claude/init.go`
- Create: `internal/translator/commandcode/claude/init_test.go`
- Modify: `internal/translator/init.go`

**Interfaces:**
- Consumes: `ConvertCommandCodeResponseToClaude` from Task 4
- Produces: registered `Claude` → `CommandCode` request/response transformers loaded by the server

- [ ] **Step 1: Write failing non-stream + register tests**

Append to `commandcode_claude_response_test.go`:

```go
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
```

Create `init_test.go`:

```go
package claude

import (
	"testing"

	. "github.com/router-for-me/CLIProxyAPI/v7/internal/constant"
	itranslator "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/translator"
)

func TestClaudeCommandCodeRegistered(t *testing.T) {
	out := itranslator.Request(Claude, CommandCode, "deepseek/deepseek-v4-pro", []byte(`{"messages":[{"role":"user","content":"Hello"}]}`), true)
	if gjson.GetBytes(out, "memory").Type != gjson.String {
		t.Fatalf("registry did not use Claude mapper: %s", out)
	}
	if !gjson.GetBytes(out, "params.messages").IsArray() {
		t.Fatalf("params.messages: %s", out)
	}
}
```

Add `"github.com/tidwall/gjson"` to that file's imports.

- [ ] **Step 2: Run and confirm fail**

```bash
go test -count=1 -run 'TestConvertCommandCodeResponseToClaudeNonStream_TextAndUsage|TestClaudeCommandCodeRegistered' ./internal/translator/commandcode/claude
```

Expected: FAIL, non-stream func and/or register missing.

- [ ] **Step 3: Implement non-stream + init + blank import**

`ConvertCommandCodeResponseToClaudeNonStream`:

1. Split `rawJSON` on `\n`.
2. For each non-empty line, call `ConvertCommandCodeResponseToClaude` with a shared `param`.
3. Also accumulate: thinking text, visible text, tool_use `{id,name,inputJSON}`, stop reason, usage — from the same event fields the stream mapper reads (`reasoning-delta`, `text-delta`, `tool-input-*`, `tool-call`, `finish`).
4. Build one object:

```json
{"id":"...","type":"message","role":"assistant","model":"...","content":[],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}
```

Content order: thinking (if any), text (if any), then each `tool_use` with parsed `input` object (or `{}` if invalid JSON).

Create `init.go`:

```go
package claude

import (
	. "github.com/router-for-me/CLIProxyAPI/v7/internal/constant"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/translator/translator"
)

func init() {
	translator.Register(
		Claude,
		CommandCode,
		ConvertClaudeRequestToCommandCode,
		interfaces.TranslateResponse{
			Stream:    ConvertCommandCodeResponseToClaude,
			NonStream: ConvertCommandCodeResponseToClaudeNonStream,
		},
	)
}
```

Do not set `TokenCount`.

In `internal/translator/init.go`, add this blank import next to the other commandcode imports:

```go
_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/commandcode/claude"
```

- [ ] **Step 4: Run package tests**

```bash
gofmt -w internal/translator/commandcode/claude internal/translator/init.go
go test -count=1 ./internal/translator/commandcode/claude ./internal/translator/commandcode/openai/chat-completions
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/translator/commandcode/claude/commandcode_claude_response.go internal/translator/commandcode/claude/commandcode_claude_response_test.go internal/translator/commandcode/claude/init.go internal/translator/commandcode/claude/init_test.go internal/translator/init.go
git commit -m "feat(commandcode): register Claude translators and fold non-stream messages"
```

---

### Task 6: Format, test, compile

**Files:** all files from Tasks 2–5

**Interfaces:**
- Consumes: completed translators
- Produces: green package tests and a compiling `./cmd/server`

- [ ] **Step 1: gofmt**

```bash
gofmt -w internal/translator/commandcode/claude internal/translator/commandcode/openai/chat-completions/commandcode_openai_request.go internal/translator/commandcode/openai/chat-completions/commandcode_openai_request_test.go internal/translator/init.go
```

- [ ] **Step 2: Run translator tests**

```bash
go test -count=1 ./internal/translator/commandcode/...
```

Expected: PASS.

- [ ] **Step 3: Compile gate**

```bash
go build -o test-output ./cmd/server && rm test-output
```

Expected: exit 0.

- [ ] **Step 4: Commit only if gofmt produced a diff**

```bash
git add -u internal/translator
git diff --cached --quiet || git commit -m "style(commandcode): gofmt Claude translator"
```

If nothing staged, skip the commit.

---

## Self-review

**Spec coverage**

| Spec requirement | Task |
|---|---|
| Valid envelope for Claude `/v1/messages` | 3, 5 |
| Dedicated both directions, no OpenAI hop | 3, 4 |
| New-turn `reasoning-delta` → thinking | 4 |
| `tool_use` / `tool_result` ↔ `tool-call` / `tool-result` | 3, 4 |
| Executor unchanged | all |
| Shared `BuildCommandCodeEnvelope` | 2 |
| Drop thinking history + images | 3 |
| Drop cache_control / tool_choice / thinking config / output_config / context_management | 3 |
| Invalid JSON still Zod-safe | 3 |
| All parts dropped → one empty user | 3 |
| Skip bad NDJSON | 4 |
| Non-stream Claude `message` | 5 |
| Register + blank import | 5 |
| No TokenCount | 5 |
| Unit tests listed in spec | 3, 4, 5 |
| WRITE check or file issue | 1 |
| gofmt + `go test` + compile | 6 |
| Gemini out of scope | no task |

**Hang note:** There is no stream-end hook on `TranslateResponse`. `message_stop` is emitted on CommandCode `finish`, which the existing OpenAI hop already depends on. No executor flush.

**Placeholders:** none.

**Type consistency:** `BuildCommandCodeEnvelope`, `ConvertTools`, `ConvertClaudeRequestToCommandCode`, `ConvertCommandCodeResponseToClaude`, `ConvertCommandCodeResponseToClaudeNonStream` names match across tasks.
