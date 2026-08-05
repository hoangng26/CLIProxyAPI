# CommandCode Responses + Interactions Support Design

**Date:** 2026-08-05  
**Status:** Approved for planning  
**Repos:** CLIProxyAPI  
**Related:** CommandCode chat-completions translator; `docs/superpowers/specs/2026-08-02-commandcode-multi-key-design.md`

## Problem

CommandCode is wired as a native provider (`/alpha/generate` + NDJSON) with translators only for OpenAI **Chat Completions** (`openai` → `commandcode`).

Codex and other clients call **OpenAI Responses** (`/v1/responses`, including websocket). Interactions clients use the **interactions** entry protocol. For those formats the registry has no `openai-response`/`interactions` → `commandcode` request (or reverse response) transformers.

`TranslateRequest` does **not** multi-hop. When no transformer is registered it returns the original payload (with model normalization). The CommandCode executor then POSTs a Responses-shaped body to `/alpha/generate`.

CommandCode validates and returns 400, for example:

```text
expected string, received undefined at "memory"
expected array, received undefined at "params.messages"
expected object, received undefined at "config"
```

Config comments still say v1 is chat/completions only, which matches the current code but blocks Codex-style use of CommandCode models.

## Goals

1. Support CommandCode for full client surface:
   - OpenAI Chat Completions (existing)
   - OpenAI Responses HTTP `/v1/responses`
   - OpenAI Responses websocket (same format after normalize)
   - Interactions entry protocol
2. Keep the CommandCode executor and upstream protocol unchanged.
3. Reuse existing chat ↔ CommandCode envelope mapping as the single source of truth for `/alpha/generate`.
4. Preserve existing chat-completions behavior and tests.
5. Update config comments to match supported surfaces.
6. Cover composition with focused unit tests and standard compile gates.

## Non-Goals

- Generic multi-hop support inside `sdk/translator` registry.
- Native dual dialects that map Responses/Interactions fields directly into CommandCode without a chat hop.
- Changing CommandCode auth, multi-key config, management API, or base URL/path.
- Perfect Responses/Interactions feature parity beyond what existing chat hops already express.
- Bridging CommandCode `threadId` / `memory` across requests.
- Management Center UI changes.
- Fixing unrelated hop bugs in Responses↔Chat or Interactions↔Chat.

## Decisions (locked)

| Decision | Choice |
|----------|--------|
| Approach | Compose through OpenAI Chat Completions (Approach 1) |
| Surfaces | Chat + Responses HTTP + Responses websocket + Interactions |
| Executor | Unchanged protocol/headers/path |
| Stream state | Dual holder: `viaChat` + `toClient` |
| Fidelity | Capped by chat hops + existing CommandCode chat mapping |
| Websocket | No CommandCode-specific WS code; registration is enough |
| Docs | Config comment update + this design spec |

## Architecture

```text
 Client formats                  Translator registry
 ─────────────────              ────────────────────
 openai  ──────────► chat ──► commandcode envelope ──► /alpha/generate
                    ▲              │
 openai-response ───┘              │ NDJSON
                    ▲              ▼
 interactions ──────┘     commandcode ──► chat ──► client format
```

### Runtime (unchanged executor shape)

1. Handler sets source/response format (`openai`, `openai-response`, or `interactions`).
2. `CommandCodeExecutor.prepareBody` calls `TranslateRequest(from, "commandcode", ...)`.
3. Registered transformers exist for all three `from` values and produce the CommandCode envelope.
4. Executor forces `params.stream=true` and POSTs `/alpha/generate`.
5. NDJSON lines (or full body for non-stream fold) go through `TranslateStream` / `TranslateNonStream` from `commandcode` to the client format.

### Surfaces

| Surface | Mechanism |
|---------|-----------|
| HTTP `/v1/chat/completions` | Existing `openai` ↔ `commandcode` |
| HTTP `/v1/responses` | New composed `openai-response` ↔ `commandcode` |
| Responses websocket | Same pair after existing normalize; no new WS code |
| Interactions HTTP | New composed `interactions` ↔ `commandcode` |

## Components and file layout

```text
internal/translator/commandcode/
  openai/
    chat-completions/          # EXISTS — envelope source of truth
    responses/                 # NEW
      init.go
      commandcode_openai-responses_request.go
      commandcode_openai-responses_response.go
      commandcode_openai-responses_request_test.go
      commandcode_openai-responses_response_test.go
  interactions/                # NEW
    init.go
    interactions_commandcode_request.go
    interactions_commandcode_response.go
    interactions_commandcode_request_test.go
    interactions_commandcode_response_test.go
```

### Wiring

`internal/translator/init.go` blank-imports:

- `commandcode/openai/responses`
- `commandcode/interactions`

### Registration

**Responses package**

- `Register(OpenaiResponse, CommandCode, request, {Stream, NonStream})`

**Interactions package**

- `Register(Interactions, CommandCode, request, {Stream, NonStream})`

Chat registration remains:

- `Register(OpenAI, CommandCode, …)` (unchanged)

### Composition map

| New function | Chain |
|--------------|-------|
| `ConvertOpenAIResponsesRequestToCommandCode` | `ConvertOpenAIResponsesRequestToOpenAIChatCompletions` → `ConvertOpenAIRequestToCommandCode` |
| `ConvertCommandCodeResponseToOpenAIResponses` | `ConvertCommandCodeResponseToOpenAI` → each chunk via `ConvertOpenAIChatCompletionsResponseToOpenAIResponses` |
| `ConvertCommandCodeResponseToOpenAIResponsesNonStream` | `ConvertCommandCodeResponseToOpenAINonStream` → `ConvertOpenAIChatCompletionsResponseToOpenAIResponsesNonStream` |
| `ConvertInteractionsRequestToCommandCode` | `ConvertInteractionsRequestToOpenAI` → `ConvertOpenAIRequestToCommandCode` |
| `ConvertCommandCodeResponseToInteractions` | `ConvertCommandCodeResponseToOpenAI` → each chunk via `ConvertOpenAIResponseToInteractions` |
| `ConvertCommandCodeResponseToInteractionsNonStream` | Chat non-stream fold → `ConvertOpenAIResponseToInteractionsNonStream` |

Dependencies (exported converters only; no import cycles):

- `internal/translator/openai/openai/responses`
- `internal/translator/openai/interactions/chat-completions`
- `internal/translator/commandcode/openai/chat-completions`

### Dual stream state

Both hops use `*param` for their own state types. Composed stream converters use one holder:

```go
type composedStreamParam struct {
    viaChat  any // CommandCode → chat hop state
    toClient any // chat → Responses or chat → Interactions state
}
```

Per NDJSON line:

1. Call hop1 with `&holder.viaChat` → 0..N chat chunks (plain JSON, no `data:` prefix).
2. For each chat chunk, call hop2 with `&holder.toClient`.
3. Flatten to `[][]byte`.

Defensive behavior: if `*param` is the wrong type, re-init the holder (same spirit as existing `ensureStreamState`); do not panic in request paths.

Non-stream composition does not share param across hops; each non-stream helper uses local state as today.

## Stream, errors, and limits

### Streaming contract

- Upstream is always NDJSON; client may be stream or non-stream (executor already folds non-stream).
- Preserve hop1 chunk order and hop2 event order within each chunk.
- Metadata-only CommandCode events that hop1 ignores stay silent (empty output).
- Do not double-wrap SSE/`data:` prefixes.

### Errors

| Layer | Behavior |
|-------|----------|
| Translation | Pure transforms; no new error types |
| Upstream 4xx/5xx | Existing executor `statusErr` path |
| Wrong dual-state type | Re-init holder; no panic |
| Empty messages after chat hop | May still 400 upstream; treated as hop fidelity limit |

### Websocket

- Existing Responses websocket normalize/repair/compaction unchanged.
- After normalize, `SourceFormat=openai-response` uses the new composed pair.
- No CommandCode thread affinity across WS turns (`threadId` remains per-request UUID as on chat path).

### Known fidelity limits (document, do not expand in this work)

1. Responses/Interactions features not expressible in OpenAI chat are dropped at the first hop.
2. CommandCode `memory` and rich `config.*` (workdir/git) stay empty defaults as on chat path.
3. `threadId` is a new UUID per request.
4. Image/file handling inherits chat CommandCode degradation.
5. Reasoning only flows as far as the chat mapping already surfaces.
6. No generic registry multi-hop beyond these explicit pairs.

### Success criteria

- Responses request with `instructions` + text `input` produces CommandCode body with string `memory`, object `config`, and array `params.messages` (no validation 400 for those three fields).
- Streamed CommandCode text/tool NDJSON becomes valid Responses `response.*` events (and Interactions events on that path).
- Existing chat-completions CommandCode tests remain green.
- Responses websocket works without new WS code once translators are registered.
- Config comments no longer claim chat-only.

## Testing

### Unit tests — Responses compose

- Simple text Responses payload → envelope has `memory`, `config`, `params.messages`, `params.model`, `params.stream`.
- Instructions + user input → system folded via chat hop; user content present.
- `function_call` + `function_call_output` in `input` → assistant tool_calls + tool messages in envelope (spot-check).
- Stream: text-delta + finish → Responses terminal/completed path; dual-state survives multiple lines.
- Stream: tool-input-start/delta + finish → function call items in Responses stream.
- Non-stream multi-line NDJSON → single Responses JSON; usage preserved when present.

### Unit tests — Interactions compose

- Request with `system_instruction` + input → envelope messages; system not entirely lost.
- Stream text + finish → Interactions events; dual-state OK.
- Non-stream fold → single interactions-shaped payload.

### Regression and gates

```bash
gofmt -w .
go test ./internal/translator/commandcode/...
go test ./internal/runtime/executor/ -run CommandCode
go build -o test-output ./cmd/server && rm test-output
```

Optional (nice-to-have): executor test with mocked upstream asserting `SourceFormat=openai-response` yields `params.messages`.

## Docs and rollout

### Docs

1. This design file under `docs/superpowers/specs/`.
2. `config.example.yaml` CommandCode comments: replace “chat/completions only” with supported surfaces and a short composition note.
3. Same comment fix where duplicated (e.g. commented blocks in operator `config.yaml` templates if still present).
4. No Management Center work.

### Rollout

1. Single change set: translators + blank imports + config comments + tests + this spec (already landed).
2. No config schema migration; existing `commandcode-api-key` model lists keep working.
3. Operator smoke: Codex `/v1/responses` with a CommandCode model (e.g. `deepseek/deepseek-v4-flash`) must not hit the three-field validation 400.
4. Websocket/Interactions covered by unit tests + optional manual smoke.

### Implementation order (for planning)

1. Responses compose (request, stream dual-state, non-stream) + tests
2. Blank import + compile
3. Interactions compose + tests
4. Config comments
5. Package tests + build gate

### Repo constraint (AGENTS.md)

Avoid translator-only changes. This work includes config comment updates and design docs, so it is not translator-only. If repository write permission is insufficient for translator paths, follow AGENTS.md (permission check / issue) before implementing.

## Open questions

None locked open for planning. Fidelity limits above are accepted for v1 of this feature.
