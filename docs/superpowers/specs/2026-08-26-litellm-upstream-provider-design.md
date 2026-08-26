# LiteLLM Upstream Provider Design

**Date:** 2026-08-26

## Goal

Add LiteLLM Proxy as a dedicated upstream provider. Users configure static LiteLLM model aliases and API-key pools. Requests reaching CLIProxyAPI's existing public APIs route to LiteLLM's matching native endpoint without translating request or response bodies between protocols.

Supported native protocol pairs:

| Public CLIProxyAPI endpoint | LiteLLM Proxy endpoint |
| --- | --- |
| `POST /v1/chat/completions` | `POST {base-url}/v1/chat/completions` |
| `POST /v1/responses` | `POST {base-url}/v1/responses` |
| `POST /v1/messages` | `POST {base-url}/v1/messages` |

LiteLLM documents all three routes, including OpenAI-compatible bearer authentication and Anthropic Messages compatibility. The provider targets LiteLLM Proxy, not LiteLLM's Python SDK.

## Scope

Included:

- Dedicated `litellm` provider configuration, auth synthesis, executor registration, and static model registration.
- Native forwarding for Chat Completions, Responses, and Messages in streaming and non-streaming modes.
- API-key pooling through existing weighted round-robin scheduling.
- Existing payload configuration and thinking pipeline at the provider boundary.
- Management API configuration CRUD, documentation, unit tests, a route-level integration test, and Management Center UI support.

Excluded:

- Remote model discovery by backend via `GET /v1/models`. The Management Center may use its existing client-side discovery workflow to populate static aliases after explicit user selection.
- LiteLLM-specific fallback, budget, cache, guardrail, or administration APIs.
- Image, audio, embeddings, batch, realtime, assistants, or Anthropic Completion endpoints.
- Generic changes to `openai-compatibility`; LiteLLM remains isolated from existing compatibility-provider behavior.

## Configuration

Introduce a top-level `litellm:` configuration block containing one or more named LiteLLM Proxy instances. Its shape follows existing named API-key-backed provider blocks:

```yaml
litellm:
  - name: production
    disabled: false
    priority: 0
    base-url: http://localhost:4000
    headers:
      X-Deployment: production
    request-retry: 2
    request-scoped-errors:
      - status: 400
        match: [invalid_request_error]
        action: stop
    api-key-entries:
      - api-key: ${LITELLM_API_KEY}
        weight: 1
        proxy-url: socks5://proxy.example:1080
    models:
      - name: openai/gpt-5
        alias: gpt-5-via-litellm
        display-name: GPT-5 through LiteLLM
        max-context-length: 400000
        input-modalities: [text, image]
        output-modalities: [text]
        thinking:
          levels: [low, medium, high]
```

`base-url` is LiteLLM Proxy root. It must contain an HTTP(S) scheme and host and must not include a terminal `/v1` path segment. Executor constructs endpoint paths itself. This prevents accidental `/v1/v1/...` requests. API-key entries inherit shared base URL and headers; each entry may override outbound proxy URL. An enabled named block must contain at least one API-key entry, while disabled blocks may be retained without credentials.

Each model is static. `name` is upstream LiteLLM model identifier. `alias` is public CLIProxyAPI model name; when omitted, `name` is used. Config validation rejects duplicate provider names and duplicate aliases within a provider, validates URL shape, and uses existing API-key weight rules. Provider options with existing semantic equivalents use same behavior: disabled state, priority, retry, request-scoped errors, cooling controls, custom headers, model capabilities, mapping, and thinking metadata.

No credentials are written to provider-specific token files. Secrets remain in config-backed auth records and existing secret-resolution paths.

## Architecture

### Configuration and auth lifecycle

1. Parser reads `litellm` into a dedicated config type.
2. Normalization validates and canonicalizes LiteLLM configuration.
3. Config synthesizer creates one in-memory auth record per API-key entry. Attributes include base URL, API key, custom headers, provider name, provider key, and config index.
4. Existing auth scheduler selects credentials by weight, cooling state, and priority.
5. Service registration binds LiteLLM auth records to `LiteLLMExecutor` under unique internal provider key `litellm-<lowercase-name>`.
6. Static aliases register under that instance provider key, making them eligible for normal model routing and `/v1/models` output. Multiple named instances remain independently schedulable even when they expose overlapping aliases.

### Request flow

1. Existing public handlers parse requests for Chat Completions, Responses, or Messages.
2. Normal routing resolves public model alias to LiteLLM provider and selected auth entry.
3. `LiteLLMExecutor` determines native target from source format and keeps executor request format equal to that source format:
   - OpenAI chat format: `/v1/chat/completions`
   - OpenAI Responses format: `/v1/responses`
   - Claude format: `/v1/messages`
4. Executor resolves configured upstream model name, runs existing payload-config and canonical thinking application where supported, and changes only fields these established rules require.
5. Remaining native request body is preserved. No protocol conversion occurs.
6. Executor posts body to `base-url + native path`, with `Content-Type: application/json`, `Authorization: Bearer <api-key>`, config headers, request headers allowed by existing custom-header rules, and existing proxy-aware transport.
7. Non-streaming success body and headers return unchanged. Streaming SSE bytes return unchanged. Existing response logging, error accounting, cancellation, and usage reporting remain active.

## Executor contract

`LiteLLMExecutor` implements existing `ProviderExecutor` contract and contains no OAuth refresh flow. `Refresh` returns existing API-key auth or delegates only through existing plugin/Home compatibility mechanism where applicable.

`RequestToFormat` returns `FormatOpenAI` for Chat Completions, `FormatOpenAIResponse` for Responses, and `FormatClaude` for Messages. Executor must preserve those formats when applying thinking and payload rules; no translator registration is needed for native source/target pairs. `CountTokens` uses local counting for the selected native format; it does not call an unscoped LiteLLM token-count endpoint.

Endpoint selection is explicit, not inferred from URL. Any source format outside Chat Completions, Responses, or Claude Messages fails before creating an upstream request with clear unsupported-format error.

The executor preserves native body shapes. It must not force Chat Completions `stream_options`, manufacture `[DONE]`, reshape SSE events, or translate Anthropic bodies through OpenAI. This protects LiteLLM-native Responses events and Messages event semantics.

## Errors and observability

- Missing or invalid base URL/API key produces existing provider-auth failure path without logging secret values.
- Upstream non-2xx responses preserve status and body for client compatibility, while error logging uses existing sanitized body summaries.
- Connection/read errors use existing executor error path and retry/cooling policy.
- Streaming response errors propagate through stream result and preserve upstream partial response behavior.
- Request/response metadata uses existing upstream audit log helpers; secret-bearing headers remain redacted by existing logging policy.
- Usage accounting parses native OpenAI Chat, OpenAI Responses, and Anthropic Messages usage formats. If no usage exists, existing reporter records request without inventing token counts.

## Management API, Management Center, and documentation

Management config list/create/update/delete endpoints gain LiteLLM parity with comparable API-key-backed providers. Management views redact API keys and preserve API-key entry weights/proxy URLs.

Management Center gains a dedicated LiteLLM provider brand, descriptor, card, icon, and localized labels. Its provider workbench maps Management API `litellm` records into typed resources. The form reuses the multi-key OpenAI-compatible experience: LiteLLM Proxy root base URL, API-key pool, custom headers, disabled state, priority, cooling control, and static model aliases. Client validation rejects base URLs ending in `/v1` and help text explains that base URL must be LiteLLM Proxy root.

Management Center connection testing uses first configured static model through LiteLLM Chat Completions. Its existing client-side model-discovery flow queries LiteLLM `/v1/models` and inserts discovered model names into static alias rows only after explicit user action. Discovery does not create backend dynamic model state.

`config.example.yaml` receives complete commented `litellm` example. `README.md` adds LiteLLM Proxy to upstream-provider support description and links LiteLLM documentation.

## Tests

### Configuration and routing

- Parse, clone, normalize, and validate LiteLLM config.
- Reject `base-url` without scheme/host and URLs ending in `/v1`.
- Synthesize keyless/key-backed auth records where supported by provider contract; verify secret data not exposed in management serialization.
- Register static aliases and resolve each alias to LiteLLM provider.
- Verify weighted API-key pools use existing scheduler behavior.

### Executor

For each source protocol, test non-streaming and streaming behavior using `httptest` server:

- Correct `/v1/chat/completions`, `/v1/responses`, and `/v1/messages` target path.
- Root base URL plus target path exactly once.
- Bearer key and configured headers applied.
- Resolved upstream model replaces public alias; other request fields survive byte-equivalent except established payload/thinking rules.
- Upstream success body, headers, status, and SSE stream bytes pass through unchanged.
- Error status/body propagation.
- Missing credentials/base URL and unsupported source format fail before upstream call.
- Native OpenAI and Anthropic usage reaches usage reporter where covered by existing helpers.

### Integration

Add route-level test with static model aliases. Call all three public endpoints and assert each hits matching LiteLLM upstream endpoint with native request body and returns native response.

### Management Center

- Transform Management API LiteLLM records to and from typed provider resources.
- Validate LiteLLM root URL, serialize API-key entries/models/headers, and preserve redacted existing credentials on edits.
- Test workbench create/update/delete mapping.
- Test Chat Completions connection-test path and `/v1/models` discovery path.
- Add localized provider labels and form help across supported locales.
- Browser-verify LiteLLM card, create/edit form, API-key editor, validation, discovery, and save behavior.

## Implementation boundaries

Add dedicated LiteLLM config, synthesizer, registration, model registration, executor, management wiring, docs, and focused tests. Reuse shared scheduler, model registry, payload rules, auth abstractions, transport, logging, usage, and endpoint handlers. Do not modify generic `openai-compatibility` behavior. Translator changes only occur if existing source-format plumbing requires registration; native forwarding does not need new protocol translators.

### Backend files and responsibilities

- `internal/config/config_types.go`, `internal/config/config.go`: define `LiteLLMProvider`, `LiteLLMAPIKey`, and `LiteLLMModel`, then expose `Config.LiteLLM []LiteLLMProvider` with YAML key `litellm`.
- `internal/config/config_normalization.go`, `internal/config/parse.go`, `internal/config/config_load.go`, `internal/config/weight.go`: trim/validate URL, names, aliases, nested credentials, and weights in both load paths and include LiteLLM in YAML weight validation and runtime weight validation.
- `internal/config/clone.go`: no custom code expected; add clone coverage proving nested LiteLLM slices/maps are independent.
- `internal/watcher/synthesizer/config.go`, `internal/watcher/clients.go`: synthesize `litellm:<name>` API-key auth records, preserve `config_index`, `provider_key`, `config_name`, base URL, proxy, headers, priority, retry/cooling metadata, and update client counts.
- `internal/watcher/diff/litellm.go`, `internal/watcher/diff/config_diff.go`, `internal/watcher/diff/model_hash.go`, `internal/modelconfig/model_hash.go`: hash and describe LiteLLM config changes without printing key material.
- `sdk/config/config.go`: export LiteLLM aliases for SDK consumers.
- `sdk/cliproxy/auth/conductor_models.go`, `sdk/cliproxy/auth/api_key_model_capabilities.go`: resolve LiteLLM config by auth index/name/key, map aliases, preserve suffixes, and compile capabilities.
- `sdk/cliproxy/service_executors.go`, `sdk/cliproxy/service_models.go`: register `LiteLLMExecutor` under `litellm-<name>` and static models under matching key.
- `internal/runtime/executor/litellm_executor.go`: native endpoint dispatch, auth/header injection, payload model rewrite, transport, passthrough response/SSE, usage parsing, local token count, refresh no-op.
- `internal/api/handlers/management/config_lists.go`, `internal/api/handlers/management/config_auth_index.go`, `internal/api/handlers/management/api_tools.go`, `internal/api/server_management.go`: management CRUD, auth-index response, per-key proxy lookup, and routes under `/v0/management/litellm`.
- `config.example.yaml`, `README.md`: operator configuration and LiteLLM documentation.

### Management Center files and responsibilities

- `src/types/provider.ts`, `src/types/config.ts`: typed LiteLLM provider/model/key records and normalized config section.
- `src/services/api/transformers.ts`, `src/services/api/providers.ts`: normalize `/config` and `/litellm`; serialize/merge/preserve redacted keys; implement get/create/update/disable/delete.
- `src/features/providers/types.ts`, `descriptors.ts`, `brandLogos.ts`, `adapters.ts`, `useProviderWorkbench.ts`: add `litellm` brand, resource selector, descriptor, adapter, snapshot, mutations, and ordering.
- `src/features/providers/sheets/forms/BaseProviderForm.tsx`, `useConnectivityTest.ts`, `useModelDiscovery.ts`, `src/components/providers/utils.ts`: reuse multi-key form; add LiteLLM root URL validation, `/v1/chat/completions` probe, and `/v1/models` discovery.
- `src/features/providers/components/ProviderResourceTable.tsx`, `ProviderSheet.tsx`, `ProvidersWorkbenchPage.tsx`, `src/router/MainRoutes.tsx`: card/resource rendering, route text, and fixed-brand compatibility if required by existing navigation.
- `src/i18n/locales/en.json`, `zh-CN.json`, `zh-TW.json`, `ru.json`: provider name, root URL help, validation, discovery, and endpoint copy.
- `tests/litellmProvider.test.ts`: focused normalization, CRUD, workbench adapter, endpoint builder, and validation tests. Browser verification covers card/form/discovery/save behavior.

## Implementation plan

The plan is split into backend and UI tracks. Backend contract must land before UI API wiring; UI visual work can proceed against the documented contract after backend types/routes are settled.

### Backend Task 1: Configuration contract and validation

Add typed LiteLLM config and model/key structures. Implement normalization and validation in both `ParseConfigBytes` and file load paths. Use root URL invariant: scheme plus host, no terminal `/v1`; trim names, remove empty key entries, preserve disabled empty blocks, reject duplicate provider names and duplicate aliases within a block. Extend weight validation. Write failing tests first in `internal/config/litellm_test.go`, then run `rtk go test ./internal/config -run LiteLLM`.

### Backend Task 2: Auth synthesis and runtime diff

Add synthesizer and client-count support. Generate stable IDs using provider name, API key, root URL, proxy URL, and headers; set provider `litellm-<name>`, attributes `provider_key`, `config_name`, `config_index`, `base_url`, `api_key`, and metadata for priority/weight/retry/cooling. Add model hash and config diff without key values. Add tests in `internal/watcher/synthesizer/litellm_test.go`, `internal/watcher/diff/litellm_test.go`, and clone coverage. Run focused watcher/config tests.

### Backend Task 3: Routing, model aliases, and executor registration

Add LiteLLM handling to auth model alias/capability compilation and service model registration. Register one executor per named provider key and ensure disabled/removed entries unregister cleanly. Add SDK config aliases. Tests in `sdk/cliproxy/auth/litellm_model_routing_test.go`, `sdk/cliproxy/service_litellm_registration_test.go`, and `sdk/cliproxy/openai_compat_config_models_test.go` extension. Verify aliases route to correct instance and overlapping aliases remain independently schedulable.

### Backend Task 4: Native LiteLLM executor

Write executor tests before code in `internal/runtime/executor/litellm_executor_test.go`. Test `RequestToFormat`, exact URL paths, root URL handling, model rewrite, bearer auth, custom headers, native body preservation, non-stream pass-through, SSE pass-through, non-2xx status/body, missing credentials, unsupported format, local token counting, and usage parsing. Implement `internal/runtime/executor/litellm_executor.go` with small helpers for source-to-path/format, credentials, model resolution, request construction, and stream copying. Use `helps.NewProxyAwareHTTPClient`, request/response recording, `NewExecutorUsageReporter`, `ParseOpenAIUsage`, `ParseClaudeUsage`, `ParseOpenAIStreamUsage`, `ParseClaudeStreamUsage`, and no synthetic stream terminal frames. Run `rtk go test ./internal/runtime/executor -run LiteLLM`.

### Backend Task 5: Management API and docs

Add redacted auth-index response types, list normalization, CRUD handlers, per-key proxy lookup, and four management routes. Preserve unknown fields on PUT/PATCH as existing provider APIs do. Add `internal/api/handlers/management/config_litellm_test.go` covering GET redaction/index, PUT/PATCH validation, DELETE by name/index, duplicate-name rejection, and key/proxy preservation. Add example YAML and README support statement. Run management focused tests and `rtk go test ./...`.

### UI Task 1: Types, transforms, API CRUD

Add LiteLLM config types and `litellm` normalization. Implement API methods against `/litellm`, preserving unknown fields and existing redacted credentials on edits. Add failing tests in `tests/litellmProvider.test.ts` for response normalization, serialization shape, merge behavior, and create/update/delete calls. Run `bun test tests/litellmProvider.test.ts`.

### UI Task 2: Provider workbench resource and descriptor

Add `litellm` to `ProviderBrand`, selectors, brand order, descriptor, logo registry, adapter, snapshot loading, create/update/delete/toggle mutations, and resource metrics. Reuse named multi-key rendering and preserve source indexes/auth indexes. Extend focused UI tests for card/resource fields and descriptor capabilities.

### UI Task 3: Form, validation, connectivity, discovery

Reuse `BaseProviderForm` and `ApiKeyEntriesEditor`. Add LiteLLM to initial form, API-key-entry handling, model editor behavior, and capability flags. Add shared root URL validator rejecting terminal `/v1`; show LiteLLM-specific help. Add endpoint helper producing exactly `{root}/v1/chat/completions`; add discovery helper producing `{root}/v1/models`. Ensure connection test uses selected static model and first credential through `/v0/management/api-call`; discovery inserts models only after explicit selection. Add tests for URL edge cases and endpoint paths.

### UI Task 4: Localization, navigation, and browser verification

Add LiteLLM labels/help/errors to all four locale files. Ensure card appears in provider category list, sheet titles/descriptions use correct route text, and route handling does not collide with generic OpenAI compatibility. Run `bun run type-check`, `bun run lint`, `bun run test`, and `bun run build`. Start UI with project preview command, browser-check create/edit/card/discovery/save/validation, then record result.

### Final integration task: Cross-repository verification

Run backend `rtk gofmt -w` on changed Go files, `rtk go test ./...`, and required `rtk go build -o cli-proxy-api ./cmd/server && rm cli-proxy-api`. Run UI `bun run verify`. Confirm backend and UI agree on `litellm` YAML key, `/v0/management/litellm` routes, provider key naming, auth-index fields, model alias fields, root URL semantics, and redacted credential behavior. Do not stage or alter pre-existing `Dockerfile`/`docker-compose.yml` changes.

## Verification matrix

| Requirement | Backend evidence | UI evidence |
| --- | --- | --- |
| Static aliases | model registration/routing tests | adapter/workbench tests |
| Weighted API-key pool | config/scheduler tests | key editor serialization tests |
| Native Chat/Responses/Messages | executor path/body/SSE tests | endpoint probe copy/tests |
| Root URL invariant | parse/management validation tests | form/helper validation tests |
| Auth and custom headers | executor tests | CRUD/connection tests |
| Error and usage handling | executor/usage tests | connection error rendering |
| Management CRUD | handler tests | API mutation tests |
| Documentation/locales | example/README review | four locale files |
| Real UI behavior | — | browser verification |

## Commit boundaries

Keep commits focused and independently testable:

1. `feat(config): add LiteLLM configuration contract`
2. `feat(auth): synthesize LiteLLM credentials`
3. `feat(routing): register LiteLLM models and executors`
4. `feat(executor): forward native LiteLLM protocols`
5. `feat(management): expose LiteLLM configuration API`
6. `feat(ui): add LiteLLM provider management`
7. `docs: document LiteLLM upstream configuration`

Use repository commit conventions and append required `Co-Authored-By` trailer. Do not commit unrelated existing changes.
