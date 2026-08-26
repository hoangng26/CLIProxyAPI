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

### Request flow

1. Existing public handlers parse requests for Chat Completions, Responses, or Messages.
2. Normal routing resolves public model alias to LiteLLM provider and selected auth entry.
3. `LiteLLMExecutor` determines native target from source format:
   - OpenAI chat format: `/v1/chat/completions`
   - OpenAI Responses format: `/v1/responses`
   - Claude format: `/v1/messages`
4. Executor resolves configured upstream model name, runs existing payload-config and canonical thinking application where supported, and changes only fields these established rules require.
5. Remaining native request body is preserved. No protocol conversion occurs.
6. Executor posts body to `base-url + native path`, with `Content-Type: application/json`, `Authorization: Bearer <api-key>`, config headers, request headers allowed by existing custom-header rules, and existing proxy-aware transport.
7. Non-streaming success body and headers return unchanged. Streaming SSE bytes return unchanged. Existing response logging, error accounting, cancellation, and usage reporting remain active.

## Executor contract

`LiteLLMExecutor` implements existing `ProviderExecutor` contract and contains no OAuth refresh flow. `Refresh` returns existing API-key auth or delegates only through existing plugin/Home compatibility mechanism where applicable.

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

## Success criteria

- User can configure static LiteLLM model aliases with weighted API keys and LiteLLM Proxy root base URL.
- Requests to all three public endpoints reach matching LiteLLM native endpoint.
- Chat, Responses, and Messages request/response/SSE bodies remain native and unconverted.
- Credentials and custom headers work without secret leakage.
- Existing provider routing, retry, cooling, payload controls, thinking pipeline, and observability operate for LiteLLM.
- Focused tests prove config validation, dispatch, forwarding, error handling, and streaming behavior.
