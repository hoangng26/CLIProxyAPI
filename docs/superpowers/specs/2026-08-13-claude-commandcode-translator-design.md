# Claude ↔ CommandCode Translator Design

**Date:** 2026-08-13  
**Status:** Approved for planning  
**Repos:** CLIProxyAPI  
**Related:** `docs/superpowers/specs/2026-08-05-commandcode-responses-interactions-design.md`

## Problem

`deepseek/deepseek-v4-pro` (and the rest of the CommandCode catalog) is served by `CommandCodeExecutor`, which POSTs to `https://api.commandcode.ai/alpha/generate`.

That API requires a Zod-validated envelope:

```text
memory: string
config: object
params.messages: array
```

Translators exist only for OpenAI Chat Completions, OpenAI Responses, and Interactions. There is no `claude` → `commandcode` request transformer and no `commandcode` → `claude` response transformer.

`TranslateRequest` does not multi-hop. Missing route = passthrough of the original payload (model field may be rewritten). `prepareBody` then only sets `params.stream = true`.

A Claude Code `/v1/messages` body therefore reaches `/alpha/generate` as Anthropic JSON plus `params.stream`. CommandCode returns 400:

```text
expected string, received undefined at "memory"
expected array, received undefined at "params.messages"
expected object, received undefined at "config"
```

CCR wraps that as `All target providers failed` on `cli-proxy-api::anthropic_messages`.

## Goals

1. Claude `/v1/messages` to any CommandCode model produces a valid envelope (`memory`, `config`, `params.messages` present).
2. Dedicated Claude ↔ CommandCode mapping in both directions. No OpenAI chat hop.
3. Stream new-turn reasoning as Claude `thinking` events (`reasoning-delta` → thinking block).
4. Map Claude `tool_use` / `tool_result` to CommandCode `tool-call` / `tool-result`, and the reverse on the stream.
5. Keep `CommandCodeExecutor` protocol, headers, and path unchanged.
6. Extract a shared envelope builder so OpenAI / Responses / Interactions / Claude do not each hard-code `threadId` / `memory` / `config`.
7. Cover the mapping with unit tests and the standard compile gate.

## Non-Goals

- Gemini → CommandCode (same missing-hop class; out of this change).
- Generic multi-hop inside `sdk/translator`.
- Passing images (omit image parts, same as the existing OpenAI hop).
- Replaying prior-turn `thinking` / `redacted_thinking` into CommandCode history (drop them).
- Mapping `cache_control`, `tool_choice`, top-level `thinking`, `output_config`, or `context_management` into the envelope (CommandCode has no fields for them).
- Bridging CommandCode `threadId` / `memory` across requests.
- Implementing `CountTokens` for CommandCode (executor stays 501).
- Refactoring `internal/translator/openai/claude` SSE helpers.
- Auth, multi-key, base URL, or management-API changes.

## Decisions (locked)

| Decision | Choice |
|----------|--------|
| Approach | Dedicated Claude maps + shared envelope (Approach 2) |
| Directions | Request and response both dedicated |
| Prior-turn thinking | Drop `thinking` / `redacted_thinking` |
| Images | Omit |
| New-turn thinking | `reasoning-delta` → Claude thinking SSE |
| Gemini | Out of scope |
| TokenCount | Omit registration |
| Executor | Unchanged |

## Architecture

```text
 Client                         Translator                         Upstream
 ──────                         ──────────                         ────────
 Claude /v1/messages ──► commandcode/claude request
                              │
                              ▼
                     shared envelope
                     + Claude message/tool map
                              │
                              ▼
                     CommandCodeExecutor ──POST /alpha/generate──► api.commandcode.ai
                              │
                              ▼ NDJSON
                     commandcode/claude response
                              │
                              ▼
                     Claude SSE / one message object
```

`CommandCodeExecutor.prepareBody` already calls `TranslateRequest(from, "commandcode", ...)`. After registration, Claude is a real route instead of passthrough.

### Shared extract

Export `BuildCommandCodeEnvelope` from `internal/translator/commandcode/openai/chat-completions`. OpenAI already lives there; Responses, Interactions, and Claude already import that package. Shape stays:

```json
{
  "threadId": "<uuid>",
  "memory": "",
  "config": {
    "workingDir": "",
    "date": "<UTC YYYY-MM-DD>",
    "environment": "<GOOS>",
    "structure": [],
    "isGitRepo": false,
    "currentBranch": "",
    "mainBranch": "",
    "gitStatus": "",
    "recentCommits": []
  },
  "params": {
    "model": "<model>",
    "stream": <bool>
  }
}
```

Anthropic-shaped `convertTools` (`name` + `input_schema`) already exists on the OpenAI hop. Export it (or a thin wrapper) from the same package so the Claude request mapper can call it. Do not rewrite tool JSON Schema.

### New package

`internal/translator/commandcode/claude/`

- `init.go` — `translator.Register(Claude, CommandCode, request, {Stream, NonStream})`
- request converter
- stream + non-stream response converters
- unit tests

Blank-import the package from `internal/translator/init.go`.

## Request mapping

1. Start from `BuildCommandCodeEnvelope(model, stream)`.
2. `params.max_tokens` from Claude `max_tokens`, else 32000.
3. `params.temperature` from Claude `temperature`, else 0.3.
4. `params.top_p` only if present.
5. `system`: string, or text parts from a content array, joined with `\n\n` → `params.system`. Drop `cache_control`.
6. `messages` in order:
   - User text → `{role:"user", content:[{type:"text", text}]}`.
   - User `tool_result` → `{role:"tool", content:[{type:"tool-result", toolCallId, toolName, output:{type:"text", value}}]}`. Backfill `toolName` from the matching prior `tool_use` when the result omits it.
   - Assistant text + `tool_use` → `{role:"assistant", content:[...]}` with `tool-call` parts (`toolCallId`, `toolName`, `input` object).
   - Drop `thinking` and `redacted_thinking`.
   - Skip image parts. If a user message has no remaining parts, skip the message.
7. `tools` → `params.tools` via existing Anthropic-shaped conversion.
8. Drop `tool_choice`, top-level `thinking`, `output_config`, `context_management`.

Invalid or empty Claude JSON still yields a valid envelope (`memory: ""`, `config: {}`, `params.messages: []`) so CommandCode Zod does not repeat the current 400. If every user part was dropped, emit one empty user text block rather than omitting `params.messages`.

## Response mapping

Dedicated state machine. Do not call `ConvertCommandCodeResponseToOpenAI` or `ConvertOpenAIResponseToClaude`.

### Stream (one NDJSON line → zero or more Claude SSE events)

| CommandCode event | Claude output |
|---|---|
| First event of the stream | `message_start` |
| `reasoning-delta` | `content_block_start` thinking if needed, then `thinking_delta` |
| `text-delta` | `content_block_start` text if needed, then `text_delta` |
| `tool-input-start` | `content_block_start` `tool_use` |
| `tool-input-delta` | `input_json_delta` |
| `tool-call` with no prior start | full `tool_use` start + input + stop |
| `finish` / `finish-step` | stop open blocks, `message_delta` (`end_turn` or `tool_use` + usage), `message_stop` |

Switching block type (thinking → text → tool) emits `content_block_stop` first.

Malformed NDJSON: skip the line. Unknown event type: ignore. If the HTTP stream ends with no `finish` / `finish-step`, close open blocks, emit `message_delta` with `stop_reason=end_turn` and `message_stop` so Claude Code does not hang.

### Non-stream

Fold the same events into one Claude `message` object: `content` is thinking + text + `tool_use` blocks, plus `stop_reason` and `usage`.

### Errors

The translator does not invent HTTP statuses. Upstream 400/401/5xx stay `statusErr` from the executor; the Claude handler already wraps them as `invalid_request_error`.

`CountTokens` stays unimplemented (501).

## Testing

Unit tests only. No live CommandCode call.

Request (`commandcode_claude_request_test.go`):

- Minimal Claude `messages` → envelope has string `memory`, object `config`, array `params.messages`, and `params.model` set.
- `system` text-block array → `params.system` joined.
- Assistant `tool_use` + user `tool_result` → `tool-call` / `tool-result` with backfilled `toolName`.
- History thinking + image part dropped; leftover text kept.
- Regression: a Claude body that previously passthrough-failed Zod now has `memory`, `config`, and `params.messages` at the required types.

Response (`commandcode_claude_response_test.go`):

- `text-delta` then `finish` → `message_start`, text block, `message_delta` `end_turn`, `message_stop`.
- `reasoning-delta` then `text-delta` → thinking block then text block, with `content_block_stop` between.
- `tool-input-start` / `tool-input-delta` / `finish` → `tool_use` and `stop_reason=tool_use`.
- Non-stream fold → one Claude `message` with text + usage.

Register: `HasRequestTransformer(Claude, CommandCode)` is true after the blank import.

Gates: `gofmt`, `go test` on the touched packages, `go build -o test-output ./cmd/server && rm test-output`.

## Repo constraint

AGENTS.md: do not make standalone `internal/translator/` changes unless `gh repo view --json viewerPermission -q .viewerPermission` is `WRITE`, `MAINTAIN`, or `ADMIN`. Otherwise file a GitHub issue with goal, rationale, and intended implementation and stop.

This change is translator-only plus the shared envelope extract. The permission check is required before implementation.

## Files (expected)

| Path | Action |
|------|--------|
| `internal/translator/commandcode/claude/init.go` | Create |
| `internal/translator/commandcode/claude/commandcode_claude_request.go` | Create |
| `internal/translator/commandcode/claude/commandcode_claude_response.go` | Create |
| `internal/translator/commandcode/claude/commandcode_claude_request_test.go` | Create |
| `internal/translator/commandcode/claude/commandcode_claude_response_test.go` | Create |
| `internal/translator/init.go` | Blank-import new package |
| `internal/translator/commandcode/openai/chat-completions/commandcode_openai_request.go` | Call shared envelope helper |
| `internal/translator/commandcode/openai/chat-completions` — export `BuildCommandCodeEnvelope` | Create/export. Claude, Responses, and Interactions already import this package, so no new package and no import cycle. |

Executor, registry models, and auth files stay unchanged.
