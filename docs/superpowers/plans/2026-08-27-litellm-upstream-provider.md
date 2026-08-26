# LiteLLM Upstream Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add dedicated, statically configured LiteLLM Proxy upstream support to CLIProxyAPI and its Management Center UI for native Chat Completions, Responses, and Anthropic Messages routes.

**Architecture:** Add a named `litellm:` provider family rather than changing generic `openai-compatibility`. Each named instance owns a root LiteLLM Proxy URL, static model aliases, shared headers, and weighted API-key entries. Auth synthesis and model registration use unique internal keys `litellm-<lowercase-name>`. A dedicated executor selects native target format/path, rewrites only resolved model and established config-controlled fields, then forwards native JSON/SSE unchanged.

**Tech Stack:** Go 1.26+, Gin, existing CLIProxyAPI auth manager/model registry/executor interfaces, `httptest`; React 19, TypeScript, Vite, Bun tests, existing provider workbench and `/v0/management` API.

**Spec:** `docs/superpowers/specs/2026-08-26-litellm-upstream-provider-design.md`

## Global Constraints

- Support only public Chat Completions, Responses, and Claude Messages native forwarding.
- Map public routes to LiteLLM `/v1/chat/completions`, `/v1/responses`, and `/v1/messages`.
- `base-url` is LiteLLM Proxy root; require HTTP(S) scheme and host; reject terminal `/v1`.
- Preserve native request bodies except model alias resolution and established payload/thinking rules.
- Preserve native non-stream responses and SSE bytes; never synthesize `[DONE]` or reshape events.
- Use `Authorization: Bearer <api-key>` by default; preserve configured custom headers and per-key proxy URL.
- Use static model configuration; no backend remote model updater.
- Do not modify generic `openai-compatibility` behavior.
- Do not expose API-key material in logs; preserve existing API-key values when UI edit fields remain blank.
- Use structured logrus logging; do not use `log.Fatal`/`log.Fatalf`.
- After Go changes run `gofmt`; required compile check is `go build -o test-output ./cmd/server && rm test-output`.
- UI user-visible copy must be added to `en`, `zh-CN`, `zh-TW`, and `ru` locales.
- Do not alter pre-existing `Dockerfile` or `docker-compose.yml` changes.
- Keep persisted prose/docs in normal prose, regardless of chat style.

---

## File map

### Backend

- Create `internal/runtime/executor/litellm_executor.go`: native LiteLLM executor and request/stream helpers.
- Create `internal/runtime/executor/litellm_executor_test.go`: executor contract tests.
- Modify `internal/config/config_types.go`: `LiteLLMProvider`, `LiteLLMAPIKey`, `LiteLLMModel`.
- Modify `internal/config/config.go`: `Config.LiteLLM []LiteLLMProvider` with YAML key `litellm`.
- Modify `internal/config/config_normalization.go`, `internal/config/parse.go`, `internal/config/config_load.go`: normalize and validate LiteLLM blocks in both load paths.
- Modify `internal/config/weight.go`: nested LiteLLM weight validation and runtime weight validation.
- Modify `internal/config/clone_test.go`: add nested clone coverage in Task 1.
- Modify `internal/watcher/synthesizer/config.go`: synthesize LiteLLM auth records.
- Create `internal/watcher/synthesizer/litellm_test.go`: synthesis tests.
- Modify `internal/watcher/clients.go`: add LiteLLM client count to `BuildAPIKeyClients` and update its sole startup-summary caller.
- Create `internal/watcher/diff/litellm.go` and `internal/watcher/diff/litellm_test.go`: stable model hash and human-readable diff.
- Modify `internal/watcher/diff/config_diff.go`: include LiteLLM diff section.
- Modify `internal/modelconfig/model_hash.go`: hash LiteLLM model type.
- Modify `sdk/config/config.go`: export LiteLLM aliases.
- Modify `internal/util/provider.go`: `LiteLLMProviderKey(name string)`.
- Modify `sdk/cliproxy/auth/conductor_models.go`: alias and upstream model resolution.
- Modify `sdk/cliproxy/auth/api_key_model_capabilities.go`: LiteLLM capability compilation.
- Modify `sdk/cliproxy/auth/conductor_execution.go`: preserve existing provider-to-format fallback; `LiteLLMExecutor.RequestToFormat` supplies LiteLLM formats, so no fallback switch change is planned.
- Modify `sdk/cliproxy/service_executors.go`: baseline/registration/cache handling for named LiteLLM instances.
- Modify `sdk/cliproxy/service_models.go`: static LiteLLM model registration.
- Create `sdk/cliproxy/auth/litellm_model_routing_test.go` and `sdk/cliproxy/service_litellm_registration_test.go`.
- Modify `internal/api/handlers/management/config_lists.go`: GET/PUT/PATCH/DELETE LiteLLM handlers.
- Modify `internal/api/handlers/management/config_auth_index.go`: auth-index response fields.
- Modify `internal/api/handlers/management/api_tools.go`: per-key LiteLLM proxy lookup.
- Modify `internal/api/server_management.go`: four `/v0/management/litellm` routes.
- Create `internal/api/handlers/management/config_litellm_test.go`.
- Modify `config.example.yaml` and `README.md`.

### Management Center

- Modify `src/types/provider.ts`, `src/types/config.ts`: LiteLLM types and config section.
- Modify `src/services/api/transformers.ts`, `src/services/api/providers.ts`: normalize and CRUD `/litellm`.
- Modify `src/features/providers/types.ts`, `descriptors.ts`, `brandLogos.ts`, `adapters.ts`: brand, selector, descriptor, logo, resource.
- Modify `src/features/providers/useProviderWorkbench.ts`: load, create, update, delete, toggle, and model resource wiring.
- Modify `src/features/providers/sheets/forms/BaseProviderForm.tsx`: LiteLLM initial values and root URL help/validation integration.
- Modify `src/features/providers/sheets/forms/useConnectivityTest.ts`: LiteLLM Chat Completions probe.
- Modify `src/features/providers/sheets/forms/useModelDiscovery.ts`: LiteLLM `/v1/models` discovery.
- Modify `src/components/providers/utils.ts`: LiteLLM endpoint builders and root URL validation.
- Modify: `src/features/providers/components/ProviderResourceTable.tsx`: add LiteLLM to named multi-key metric/status branches.
- Modify: `src/features/providers/sheets/ResourceDetailView.tsx`: add LiteLLM to named multi-key API-key/usage detail branches.
- Do not modify: `src/features/providers/sheets/ProviderSheet.tsx`; its existing default route description produces `/ai-providers/litellm`.
- Modify: `src/features/providers/ProvidersWorkbenchPage.tsx`: add LiteLLM to recent-usage named multi-key handling.
- Do not modify: `src/router/MainRoutes.tsx`; existing `/ai-providers` route renders all provider brands.
- Modify all `src/i18n/locales/{en,zh-CN,zh-TW,ru}.json`.
- Create `tests/litellmProvider.test.ts`.

---

## Backend Tasks

### Task 1: Add LiteLLM config types and validation

**Files:**
- Modify: `internal/config/config_types.go` near `CommandCodeProvider` and `OpenAICompatibility` types.
- Modify: `internal/config/config.go` near `OpenAICompatibility` field.
- Modify: `internal/config/config_normalization.go` near `SanitizeOpenAICompatibility`.
- Modify: `internal/config/parse.go` and `internal/config/config_load.go` in sanitization pipelines.
- Modify: `internal/config/weight.go` in YAML and typed weight validators.
- Create: `internal/config/litellm_test.go`.
- Modify: `internal/config/clone_test.go`.

**Interfaces:**
- Produces `config.LiteLLMProvider`, `config.LiteLLMAPIKey`, `config.LiteLLMModel`.
- Produces `(*Config).SanitizeLiteLLM()` and `(*Config).ValidateLiteLLMProviders() error`.
- `Config.LiteLLM` is `[]LiteLLMProvider` and YAML key is `litellm`.

- [ ] **Step 1: Write failing config tests.**

```go
func TestParseConfigBytesLiteLLM(t *testing.T) {
    cfg, err := ParseConfigBytes([]byte(`
        litellm:
          - name: production
            base-url: http://localhost:4000/
            api-key-entries:
              - api-key: key-1
                weight: 2
            models:
              - name: openai/gpt-5
                alias: public-gpt
    `))
    if err != nil { t.Fatal(err) }
    if len(cfg.LiteLLM) != 1 || cfg.LiteLLM[0].Name != "production" { t.Fatalf("unexpected providers: %+v", cfg.LiteLLM) }
    if cfg.LiteLLM[0].BaseURL != "http://localhost:4000" { t.Fatalf("base URL = %q", cfg.LiteLLM[0].BaseURL) }
}

func TestParseConfigBytesRejectsLiteLLMBaseURLWithV1(t *testing.T) {
    _, err := ParseConfigBytes([]byte(`
        litellm:
          - name: production
            base-url: http://localhost:4000/v1
            api-key-entries: [{api-key: key}]
    `))
    if err == nil || !strings.Contains(err.Error(), "base-url") { t.Fatalf("error = %v", err) }
}

func TestParseConfigBytesAcceptsLiteLLMAliasPool(t *testing.T) {
    aliasPool := []byte(`litellm:
      - name: prod
        base-url: http://one
        api-key-entries: [{api-key: a}]
        models: [{name: one, alias: same}, {name: two, alias: same}]`)
    cfg, err := ParseConfigBytes(aliasPool)
    if err != nil || len(cfg.LiteLLM[0].Models) != 2 { t.Fatalf("alias pool rejected: cfg=%+v err=%v", cfg, err) }
}
```

- [ ] **Step 2: Run focused tests and verify failure.**

Run:

```bash
rtk go test ./internal/config -run LiteLLM
```

Expected: FAIL because LiteLLM types and parse path do not exist.

- [ ] **Step 3: Implement typed config.**

Use these fields and tags:

```go
type LiteLLMProvider struct {
    Name string `yaml:"name" json:"name"`
    Priority int `yaml:"priority,omitempty" json:"priority,omitempty"`
    Disabled bool `yaml:"disabled,omitempty" json:"disabled,omitempty"`
    Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`
    BaseURL string `yaml:"base-url" json:"base-url"`
    APIKeyEntries []LiteLLMAPIKey `yaml:"api-key-entries,omitempty" json:"api-key-entries,omitempty"`
    Models []LiteLLMModel `yaml:"models,omitempty" json:"models,omitempty"`
    Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
    DisableCooling *bool `yaml:"disable-cooling,omitempty" json:"disable-cooling,omitempty"`
    RequestRetry *int `yaml:"request-retry,omitempty" json:"request-retry,omitempty"`
    RequestScopedErrors []RequestScopedErrorRule `yaml:"request-scoped-errors,omitempty" json:"request-scoped-errors,omitempty"`
}

type LiteLLMAPIKey struct {
    APIKey string `yaml:"api-key" json:"api-key"`
    Weight *int `yaml:"weight,omitempty" json:"weight,omitempty"`
    ProxyURL string `yaml:"proxy-url,omitempty" json:"proxy-url,omitempty"`
}

type LiteLLMModel struct {
    Name string `yaml:"name" json:"name"`
    Alias string `yaml:"alias,omitempty" json:"alias,omitempty"`
    DisplayName string `yaml:"display-name,omitempty" json:"display-name,omitempty"`
    MaxContextLength int `yaml:"max-context-length,omitempty" json:"max-context-length,omitempty"`
    ForceMapping bool `yaml:"force-mapping,omitempty" json:"force-mapping,omitempty"`
    InputModalities []string `yaml:"input-modalities,omitempty" json:"input-modalities,omitempty"`
    OutputModalities []string `yaml:"output-modalities,omitempty" json:"output-modalities,omitempty"`
    IsCompat bool `yaml:"is-compat,omitempty" json:"is-compat,omitempty"`
    Thinking *registry.ThinkingSupport `yaml:"thinking,omitempty" json:"thinking,omitempty"`
}
```

Add getters on `LiteLLMModel` matching `modelEntry`, `modelMaxContextLengthEntry`, `modelCompatEntry`, and `GetThinking`.

- [ ] **Step 4: Implement normalization and validation.**

`SanitizeLiteLLM` must:

1. Trim provider name, prefix, base URL, headers, key, and per-key proxy URL.
2. Remove empty key entries.
3. Normalize headers with `NormalizeHeaders`.
4. Trim model names/aliases and remove empty model rows.
5. Keep disabled named blocks even with no keys; drop enabled blocks with zero keys.
6. Reject duplicate provider names case-insensitively.
7. Preserve alias pools: retain duplicate aliases when upstream `name` values differ; remove duplicate identical `(name, alias)` pairs case-insensitively.
8. Parse URL using `net/url`; require `http` or `https`, non-empty host, and path not equal to `/v1` after trimming slashes.
9. Store normalized root URL without trailing slash.

Call it from both load/parse pipelines before return. Extend YAML nested weight validator with `litellm` and `ValidateCredentialWeights` with `cfg.LiteLLM` paths.

- [ ] **Step 5: Add clone coverage and rerun.**

```go
func TestCloneForRuntimeCopiesLiteLLM(t *testing.T) {
    cfg := &Config{LiteLLM: []LiteLLMProvider{{
        Name: "prod", BaseURL: "http://localhost:4000",
        Headers: map[string]string{"X-Test": "one"},
        APIKeyEntries: []LiteLLMAPIKey{{APIKey: "key"}},
        Models: []LiteLLMModel{{Name: "upstream", Alias: "public"}},
    }}}
    clone := cfg.CloneForRuntime()
    clone.LiteLLM[0].Headers["X-Test"] = "two"
    clone.LiteLLM[0].APIKeyEntries[0].APIKey = "changed"
    clone.LiteLLM[0].Models[0].Alias = "changed"
    if cfg.LiteLLM[0].Headers["X-Test"] != "one" || cfg.LiteLLM[0].APIKeyEntries[0].APIKey != "key" || cfg.LiteLLM[0].Models[0].Alias != "public" { t.Fatal("clone shares nested LiteLLM state") }
}
```

Run:

```bash
rtk go test ./internal/config -run 'LiteLLM|CloneForRuntime'
```

Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
rtk git add internal/config/config_types.go internal/config/config.go internal/config/config_normalization.go internal/config/parse.go internal/config/config_load.go internal/config/weight.go internal/config/litellm_test.go internal/config/clone_test.go
rtk git commit -m "feat(config): add LiteLLM configuration contract" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 2: Synthesize LiteLLM auth and runtime diff data

**Files:**
- Modify: `internal/watcher/synthesizer/config.go` in `Synthesize` and provider synth helpers.
- Modify: `internal/watcher/clients.go` to add a LiteLLM count to `BuildAPIKeyClients` return values and startup summary text.
- Create: `internal/watcher/synthesizer/litellm_test.go`.
- Create: `internal/watcher/diff/litellm.go` and `internal/watcher/diff/litellm_test.go`.
- Modify: `internal/watcher/diff/config_diff.go` and `internal/modelconfig/model_hash.go`.
- Modify: `internal/config/clone_test.go` for nested clone coverage.

**Interfaces:**
- Produces one `*coreauth.Auth` per non-empty key entry.
- Provider key format: `litellm-<lowercase-name>`.
- Auth attributes: `source`, `api_key`, `base_url`, `config_name`, `provider_key`, `config_index`, optional `priority`, `weight`, `models_hash`, serialized custom headers.
- Auth metadata: optional `disable_cooling`, `request_retry`, and request-scoped-error data.

- [ ] **Step 1: Write failing synthesis tests.**

```go
func TestSynthesizeLiteLLMKeys(t *testing.T) {
    weight := 2
    cfg := &config.Config{LiteLLM: []config.LiteLLMProvider{{
        Name: "Production", BaseURL: "http://localhost:4000",
        Priority: 3, APIKeyEntries: []config.LiteLLMAPIKey{
            {APIKey: "key-a", Weight: &weight, ProxyURL: "direct"},
            {APIKey: "key-b"},
        },
        Models: []config.LiteLLMModel{{Name: "upstream", Alias: "public"}},
    }}}
    auths, err := NewConfigSynthesizer().Synthesize(&SynthesisContext{Config: cfg, Now: time.Now(), IDGenerator: NewStableIDGenerator()})
    if err != nil { t.Fatal(err) }
    if len(auths) != 2 { t.Fatalf("auth count = %d", len(auths)) }
    if auths[0].Provider != "litellm-production" { t.Fatalf("provider = %q", auths[0].Provider) }
    if auths[0].Attributes["config_name"] != "Production" || auths[0].Attributes["provider_key"] != "litellm-production" { t.Fatalf("attrs = %#v", auths[0].Attributes) }
    if auths[0].Attributes["api_key"] != "key-a" || auths[0].ProxyURL != "direct" { t.Fatalf("credential attrs = %#v proxy=%q", auths[0].Attributes, auths[0].ProxyURL) }
}
```

- [ ] **Step 2: Run test to verify failure.**

```bash
rtk go test ./internal/watcher/synthesizer -run LiteLLM
```

Expected: FAIL because no LiteLLM synthesis exists.

- [ ] **Step 3: Implement synthesis.**

Mirror named `CommandCodeProvider` synthesis, but use LiteLLM names and stable ID kind `litellm:<lowercase-name>`. Skip disabled blocks. Preserve config index. Add `diff.ComputeLiteLLMModelsHash` and config diff fields without key values. Extend `BuildAPIKeyClients` with a dedicated LiteLLM count and update its sole caller in `internal/watcher/clients.go` plus startup summary text; cover count in watcher tests.

- [ ] **Step 4: Add diff/hash tests.**

```go
func TestDiffLiteLLMDoesNotPrintKey(t *testing.T) {
    oldList := []config.LiteLLMProvider{{Name: "prod", BaseURL: "http://one", APIKeyEntries: []config.LiteLLMAPIKey{{APIKey: "secret-old"}}}}
    newList := []config.LiteLLMProvider{{Name: "prod", BaseURL: "http://two", APIKeyEntries: []config.LiteLLMAPIKey{{APIKey: "secret-new"}}}}
    joined := strings.Join(DiffLiteLLM(oldList, newList), "\n")
    if strings.Contains(joined, "secret-old") || strings.Contains(joined, "secret-new") { t.Fatalf("diff leaked key: %s", joined) }
    if !strings.Contains(joined, "base-url") { t.Fatalf("diff omitted base URL change: %s", joined) }
}
```

- [ ] **Step 5: Run focused tests.**

```bash
rtk go test ./internal/watcher/synthesizer ./internal/watcher/diff ./internal/modelconfig
```

Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
rtk git add internal/watcher/synthesizer/config.go internal/watcher/synthesizer/litellm_test.go internal/watcher/clients.go internal/watcher/diff/litellm.go internal/watcher/diff/config_diff.go internal/watcher/diff/litellm_test.go internal/modelconfig/model_hash.go
rtk git commit -m "feat(auth): synthesize LiteLLM credentials" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 3: Register LiteLLM aliases, capabilities, and executors

**Files:**
- Modify: `sdk/config/config.go`.
- Modify: `sdk/cliproxy/auth/conductor_models.go`.
- Modify: `sdk/cliproxy/auth/api_key_model_capabilities.go`.
- Modify: `sdk/cliproxy/service_executors.go`.
- Modify: `sdk/cliproxy/service_models.go`.
- Create: `sdk/cliproxy/auth/litellm_model_routing_test.go`.
- Create: `sdk/cliproxy/service_litellm_registration_test.go`.

**Interfaces:**
- `LiteLLMExecutor.Identifier()` returns the exact internal provider key, such as `litellm-production`.
- `LiteLLMExecutor.RequestToFormat(req, opts)` returns `FormatOpenAI`, `FormatOpenAIResponse`, or `FormatClaude` based on `opts.SourceFormat`.
- `resolveLiteLLMProviderConfig(cfg, auth)` returns `*config.LiteLLMProvider`.
- `buildLiteLLMConfigModels(provider)` returns `[]*ModelInfo`.

- [ ] **Step 1: Write failing routing/registration tests.**

```go
func TestLiteLLMAliasResolvesToUpstreamModel(t *testing.T) {
    cfg := &internalconfig.Config{LiteLLM: []internalconfig.LiteLLMProvider{{
        Name: "prod", BaseURL: "http://localhost:4000",
        APIKeyEntries: []internalconfig.LiteLLMAPIKey{{APIKey: "key"}},
        Models: []internalconfig.LiteLLMModel{{Name: "openai/gpt-5", Alias: "public-gpt"}},
    }}}
    auth := &Auth{ID: "a", Provider: "litellm-prod", Attributes: map[string]string{
        "api_key": "key", "base_url": "http://localhost:4000", "config_name": "prod", "config_index": "0", "provider_key": "litellm-prod",
    }}
    if got := resolveUpstreamModelForLiteLLM(cfg, auth, "public-gpt"); got != "openai/gpt-5" { t.Fatalf("model = %q", got) }
}

func TestLiteLLMProviderKeyIsIndependentPerNamedInstance(t *testing.T) {
    if got := util.LiteLLMProviderKey("Production"); got != "litellm-production" { t.Fatalf("key = %q", got) }
    if got := util.LiteLLMProviderKey("staging"); got == "litellm-production" { t.Fatalf("keys collide") }
}
```

- [ ] **Step 2: Run tests to verify failure.**

```bash
rtk go test ./sdk/cliproxy/auth ./sdk/cliproxy -run LiteLLM
```

Expected: FAIL because resolver, provider key, registration, and model builder do not exist.

- [ ] **Step 3: Add provider key and resolver.**

Add `internal/util.LiteLLMProviderKey(name string) string`, lowercasing and trimming name, returning `litellm-<name>`. Do not reuse `OpenAICompatibleProviderKey`, since that would route through generic compatibility detection.

In auth model routing:

1. Recognize `litellm-` provider keys as configured model-routing auth.
2. Resolve by `config_index` first, then `config_name`, then base URL/key matching.
3. Compile aliases with suffix fallback and preserve requested thinking suffix.
4. Resolve upstream model for every alias; when alias omitted, name maps to itself.
5. Compile capabilities using LiteLLM model metadata: default `ThinkingSupport{Levels: []string{"low","medium","high"}}` for non-image models with no explicit `Thinking`, exactly matching `compileOpenAICompatibleModelCapabilities`'s existing default.

- [ ] **Step 4: Add service model builder and executor registration.**

Add a LiteLLM branch before generic compatibility default in `registerModelsForAuth`. Build each model with owned-by provider name, model type `litellm`, alias ID, display name, context metadata, modalities, `IsCompat`, and normalized thinking.

In executor registration:

1. Do not add LiteLLM to `baselineExecutorAuths`; each named instance's executor registers only from its synthesized config auth, never from a single ambiguous placeholder auth.
2. Detect `strings.HasPrefix(strings.ToLower(a.Provider), "litellm-")`.
3. Register `executor.NewLiteLLMExecutor(a.Provider, cfg)` under exact provider key.
4. Ensure removed/disabled provider auth unregisters old executor and model client.
5. Keep plugin compatibility logic from treating LiteLLM as generic OpenAI compatibility.

- [ ] **Step 5: Test model routing and registration.**

Assert aliases register under `litellm-prod`, `authManager.Executor("litellm-prod")` is a `*executor.LiteLLMExecutor`, and two named providers retain distinct executor/model provider keys.

- [ ] **Step 6: Run focused tests and commit.**

```bash
rtk go test ./sdk/cliproxy/auth ./sdk/cliproxy
rtk git add internal/util/provider.go sdk/config/config.go sdk/cliproxy/auth/conductor_models.go sdk/cliproxy/auth/api_key_model_capabilities.go sdk/cliproxy/service_executors.go sdk/cliproxy/service_models.go sdk/cliproxy/auth/litellm_model_routing_test.go sdk/cliproxy/service_litellm_registration_test.go
rtk git commit -m "feat(routing): register LiteLLM models and executors" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 4: Implement native LiteLLM executor

**Files:**
- Create: `internal/runtime/executor/litellm_executor.go`.
- Create: `internal/runtime/executor/litellm_executor_test.go`.

**Interfaces:**
- `NewLiteLLMExecutor(provider string, cfg *config.Config) *LiteLLMExecutor`.
- `Identifier() string` returns provider key.
- `PrepareRequest(*http.Request, *auth.Auth) error` applies bearer auth and custom headers.
- `HttpRequest(context.Context, *auth.Auth, *http.Request) (*http.Response, error)` uses existing proxy-aware client.
- `Execute`, `ExecuteStream`, `CountTokens`, and `Refresh` implement `ProviderExecutor`.
- Internal `liteLLMTargetForFormat(format, stream) (path string, ok bool)` maps formats to paths.

- [ ] **Step 1: Write failing format/path tests.**

```go
func TestLiteLLMTargetForFormat(t *testing.T) {
    cases := []struct { format sdktranslator.Format; path string }{
        {sdktranslator.FormatOpenAI, "/v1/chat/completions"},
        {sdktranslator.FormatOpenAIResponse, "/v1/responses"},
        {sdktranslator.FormatClaude, "/v1/messages"},
    }
    for _, tc := range cases {
        path, ok := liteLLMTargetForFormat(tc.format)
        if !ok || path != tc.path { t.Fatalf("format %q = %q, %v", tc.format, path, ok) }
    }
    if _, ok := liteLLMTargetForFormat(sdktranslator.FormatGemini); ok { t.Fatal("Gemini format accepted") }
}

func TestLiteLLMExecutorRequestToFormat(t *testing.T) {
    e := NewLiteLLMExecutor("litellm-prod", &config.Config{})
    for _, tc := range []struct { source, want string }{
        {"openai", "openai"}, {"openai-response", "openai-response"}, {"claude", "claude"},
    } {
        got := e.RequestToFormat(cliproxyexecutor.Request{}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString(tc.source)})
        if got.String() != tc.want { t.Fatalf("source %q => %q", tc.source, got) }
    }
}
```

- [ ] **Step 2: Write failing `httptest` forwarding tests.**

```go
func TestLiteLLMExecutorForwardsNativeRequests(t *testing.T) {
    type received struct { path string; body []byte; authorization string; custom string }
    receivedCh := make(chan received, 3)
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        body, _ := io.ReadAll(r.Body)
        receivedCh <- received{r.URL.Path, body, r.Header.Get("Authorization"), r.Header.Get("X-Test")}
        w.Header().Set("Content-Type", r.Header.Get("Accept"))
        if r.URL.Path == "/v1/messages" { _, _ = w.Write([]byte(`{"type":"message","usage":{"input_tokens":1,"output_tokens":2}}`)); return }
        _, _ = w.Write([]byte(`{"id":"ok","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
    }))
    defer server.Close()

    cfg := &config.Config{}
    e := NewLiteLLMExecutor("litellm-prod", cfg)
    auth := &cliproxyauth.Auth{Provider: "litellm-prod", Attributes: map[string]string{
        "base_url": server.URL, "api_key": "secret", "config_name": "prod", "provider_key": "litellm-prod",
        "config_index": "0", "header:X-Test": "yes",
    }}
    for _, tc := range []struct { source sdktranslator.Format; path string; body string }{
        {sdktranslator.FormatOpenAI, "/v1/chat/completions", `{"model":"upstream","messages":[{"role":"user","content":"hi"}],"extra":{"keep":true}}`},
        {sdktranslator.FormatOpenAIResponse, "/v1/responses", `{"model":"upstream","input":"hi","extra":{"keep":true}}`},
        {sdktranslator.FormatClaude, "/v1/messages", `{"model":"upstream","max_tokens":8,"messages":[{"role":"user","content":"hi"}],"extra":{"keep":true}}`},
    } {
        _, err := e.Execute(context.Background(), auth, cliproxyexecutor.Request{Model: "upstream", Payload: []byte(tc.body)}, cliproxyexecutor.Options{SourceFormat: tc.source, ResponseFormat: tc.source, OriginalRequest: []byte(tc.body)})
        if err != nil { t.Fatalf("source %q: %v", tc.source, err) }
        got := <-receivedCh
        if got.path != tc.path || got.authorization != "Bearer secret" || got.custom != "yes" { t.Fatalf("received = %+v", got) }
        if gjson.GetBytes(got.body, "extra.keep").Bool() != true { t.Fatalf("native field lost: %s", got.body) }
    }
}
```

- [ ] **Step 3: Write failing stream pass-through and error tests.**

Use one SSE body per native format, including Responses event names and Anthropic `message_start`/`message_delta`. Assert concatenated output equals exact upstream bytes. Return HTTP 429 with JSON body and assert `statusErr` reports 429 and body text without rewriting.

- [ ] **Step 4: Implement minimal executor.**

Implementation rules:

1. Read `base_url` and `api_key` from auth attributes; fail with `401` status error when missing.
2. Resolve selected config by provider key/config index for model alias and payload rules.
3. Strip only provider prefix from model before resolution as existing auth manager already prepares model.
4. Set request model to resolved upstream model with `sjson.SetBytes`; preserve all other JSON fields.
5. Apply `helps.ApplyRequestThinking` and `helps.ApplyPayloadConfigWithRequest` using same source/target format. Do not call cross-format translators.
6. Build URL with `strings.TrimSuffix(baseURL, "/") + nativePath`.
7. Set `Content-Type: application/json`, `Authorization: Bearer`, `User-Agent: cli-proxy-litellm`, and `util.ApplyCustomHeadersFromAttrs(req, auth.Attributes, opts.Headers)`. Config synthesis stores each header under `header:<name>`, matching `addConfigHeadersToAttrs`.
8. Use `helps.NewProxyAwareHTTPClient(ctx, cfg, auth, 0)` and `reporter.TrackHTTPClient`.
9. Non-stream: read body, record metadata/chunks, publish `ParseOpenAIUsage` for OpenAI formats and `ParseClaudeUsage` for Claude, return body unchanged.
10. Stream: copy `Read` chunks or complete SSE frames as received; do not parse/reframe/translate; inspect lines only for usage using native parsers; propagate scanner/read errors.
11. `RequestToFormat` must return source format for exactly three supported formats.
12. `CountTokens` performs local counting: `CountOpenAIChatTokens` for OpenAI Chat/Responses payloads and `CountClaudeInputTokens` for Claude payloads, then `TranslateTokenCount` only when downstream handler explicitly asks for another response representation.
13. `Refresh` delegates `helps.RefreshAuthViaHome` when handled; otherwise returns auth unchanged.

- [ ] **Step 5: Run focused executor tests.**

```bash
rtk go test ./internal/runtime/executor -run LiteLLM
```

Expected: PASS, including exact native body/SSE assertions.

- [ ] **Step 6: Commit.**

```bash
rtk git add internal/runtime/executor/litellm_executor.go internal/runtime/executor/litellm_executor_test.go
rtk git commit -m "feat(executor): forward native LiteLLM protocols" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 5: Add backend Management API and documentation

**Files:**
- Modify: `internal/api/handlers/management/config_lists.go`.
- Modify: `internal/api/handlers/management/config_auth_index.go`.
- Modify: `internal/api/handlers/management/api_tools.go`.
- Modify: `internal/api/server_management.go`.
- Create: `internal/api/handlers/management/config_litellm_test.go`.
- Modify: `config.example.yaml`, `README.md`.

**Interfaces:**
- GET `/v0/management/litellm` returns `{ "litellm": [...] }` with `auth-index` fields and API keys following existing management contract.
- PUT `/v0/management/litellm` accepts array or `{ "items": [...] }`.
- PATCH `/v0/management/litellm` accepts `{ "name": ..., "index": ..., "value": ... }`.
- DELETE `/v0/management/litellm?name=...` or `?index=...`.

- [ ] **Step 1: Write failing handler tests.**

```go
func TestLiteLLMManagementCRUD(t *testing.T) {
    h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
    // Register GET/PUT/PATCH/DELETE in gin router exactly as server_management.go does.
    // PUT a named block, GET it, PATCH its URL, DELETE by name, and assert config state.
}

func TestLiteLLMManagementRejectsV1RootAndDuplicateNames(t *testing.T) {
    // PUT invalid arrays, assert 400 and no config mutation.
}
```

Also assert GET includes `auth-index` per key and preserves weights/proxy URLs. Match current `openAICompatibilityWithAuthIndex` response policy exactly: management GET may include configured key values for existing authenticated management clients, while UI masks them and blank edit fields preserve them. Do not invent a new secret policy.

- [ ] **Step 2: Run tests to verify failure.**

```bash
rtk go test ./internal/api/handlers/management -run LiteLLM
```

Expected: FAIL because routes and handlers do not exist.

- [ ] **Step 3: Implement management handlers.**

Mirror named `CommandCode` handlers and OpenAI compatibility handlers, but use `h.cfg.LiteLLM`, `normalizeLiteLLMProvider`, `SanitizeLiteLLM`, and LiteLLM stable ID kind. Preserve unknown provider/model/key fields on update with existing normalized merge helpers. Validate all weights and URL/name/alias invariants before assigning config. Add `litellmWithAuthIndex` response structs with key-entry auth indexes.

Add per-key proxy lookup in `api_tools.go` keyed by auth provider `litellm-<name>`, auth `config_name`, `config_index`, and API key.

Register:

```go
mgmt.GET("/litellm", s.mgmt.GetLiteLLM)
mgmt.PUT("/litellm", s.mgmt.PutLiteLLM)
mgmt.PATCH("/litellm", s.mgmt.PatchLiteLLM)
mgmt.DELETE("/litellm", s.mgmt.DeleteLiteLLM)
```

- [ ] **Step 4: Add example and README copy.**

Add commented `litellm:` list example to `config.example.yaml`, including root URL, key weights/proxies, static aliases, headers, retry/cooling, and native endpoint note. Update README overview with LiteLLM Proxy link: `https://docs.litellm.ai/docs/`.

- [ ] **Step 5: Run focused tests and commit.**

```bash
rtk go test ./internal/api/handlers/management ./internal/config ./internal/watcher/synthesizer
rtk git add internal/api/handlers/management/config_lists.go internal/api/handlers/management/config_auth_index.go internal/api/handlers/management/api_tools.go internal/api/server_management.go internal/api/handlers/management/config_litellm_test.go config.example.yaml README.md
rtk git commit -m "feat(management): expose LiteLLM configuration API" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Management Center Tasks

### Task 6: Add typed LiteLLM API normalization and CRUD

**Files:**
- Modify: `src/types/provider.ts`.
- Modify: `src/types/config.ts`.
- Modify: `src/services/api/transformers.ts`.
- Modify: `src/services/api/providers.ts`.
- Create: `tests/litellmProvider.test.ts`.

**Interfaces:**

```ts
export interface LiteLLMProviderConfig {
  name: string;
  prefix?: string;
  baseUrl: string;
  apiKeyEntries: ApiKeyEntry[];
  disabled?: boolean;
  headers?: Record<string, string>;
  models?: ModelAlias[];
  priority?: number;
  disableCooling?: boolean;
  requestRetry?: number;
  requestScopedErrors?: RequestScopedErrorRule[];
  authIndex?: string;
  sourceIndex?: number;
}
```

Use `Config.litellm?: LiteLLMProviderConfig[]`. Backend JSON fields remain kebab-case: `base-url`, `api-key-entries`, `proxy-url`, `disable-cooling`, `request-retry`, and `request-scoped-errors`.

- [ ] **Step 1: Write failing UI transform/API tests.**

```ts
import { afterEach, describe, expect, test } from 'bun:test';
import { apiClient } from '../src/services/api/client';
import { providersApi } from '../src/services/api/providers';
import { normalizeConfigResponse } from '../src/services/api/transformers';

const originalGet = apiClient.get;
const originalPut = apiClient.put;
const originalDelete = apiClient.delete;

afterEach(() => {
  apiClient.get = originalGet;
  apiClient.put = originalPut;
  apiClient.delete = originalDelete;
});

test('normalizes LiteLLM records and preserves auth indexes', () => {
  const config = normalizeConfigResponse({ litellm: [{
    name: 'production',
    'base-url': 'http://localhost:4000',
    'api-key-entries': [{ 'api-key': 'secret', weight: 2, 'proxy-url': 'direct', 'auth-index': 'a1' }],
    models: [{ name: 'openai/gpt-5', alias: 'public-gpt' }],
  }] });
  expect(config.litellm?.[0]).toEqual({
    name: 'production', baseUrl: 'http://localhost:4000',
    apiKeyEntries: [{ apiKey: 'secret', weight: 2, proxyUrl: 'direct' }],
    models: [{ name: 'openai/gpt-5', alias: 'public-gpt' }], sourceIndex: 0,
  });
});

test('LiteLLM CRUD uses /litellm and preserves unrelated raw fields', async () => {
  const calls: Array<{ method: string; url: string; data?: unknown }> = [];
  apiClient.get = (async (url: string) => { calls.push({ method: 'GET', url }); return { litellm: [{ name: 'old', 'base-url': 'http://old', 'future-field': true }] }; }) as typeof apiClient.get;
  apiClient.put = (async (url: string, data?: unknown) => { calls.push({ method: 'PUT', url, data }); }) as typeof apiClient.put;
  apiClient.delete = (async (url: string) => { calls.push({ method: 'DELETE', url }); }) as typeof apiClient.delete;
  await providersApi.createLiteLLMProvider({ name: 'new', baseUrl: 'http://new', apiKeyEntries: [{ apiKey: 'key' }] });
  await providersApi.deleteLiteLLMProvider('new');
  expect(calls[1]?.url).toBe('/litellm');
  expect(calls[1]?.data).toEqual(expect.arrayContaining([expect.objectContaining({ name: 'old', 'future-field': true })]));
  expect(calls[2]).toEqual({ method: 'DELETE', url: '/litellm?name=new' });
});
```

- [ ] **Step 2: Run tests to verify failure.**

```bash
cd /Users/MAC/Workspace/Cli-Proxy-API-Management-Center
bun test tests/litellmProvider.test.ts
```

Expected: FAIL because types, normalizer, and API methods do not exist.

- [ ] **Step 3: Implement types and transformer.**

Add `normalizeLiteLLMProvider` using `normalizeApiKeyEntry`, `normalizeModelAliases`, headers, optional booleans/numbers, source index, and auth index. Add `litellm` parsing beside `openai-compatibility`. Preserve `auth-index` only as UI metadata, not in serialized config key entries.

- [ ] **Step 4: Implement API methods and merge behavior.**

Add field constants for LiteLLM. Add `serializeLiteLLMProvider`, `mergeLiteLLMProviderPayload`, `getLiteLLMProviders`, `createLiteLLMProvider`, `updateLiteLLMProvider(name, index, provider)`, `updateLiteLLMProviderDisabled(index, disabled)`, and `deleteLiteLLMProvider(name?, index?)`. Use `/config` read-before-write, preserve unknown fields and existing API-key values when edit input is blank with `mergeKnownRecordList`, exactly as OpenAI/CommandCode methods do.

- [ ] **Step 5: Run tests and commit.**

```bash
bun test tests/litellmProvider.test.ts
rtk git add src/types/provider.ts src/types/config.ts src/services/api/transformers.ts src/services/api/providers.ts tests/litellmProvider.test.ts
rtk git commit -m "feat(ui): add LiteLLM provider API contract" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 7: Add LiteLLM workbench resource, descriptor, and mutations

**Files:**
- Modify: `src/features/providers/types.ts`.
- Modify: `src/features/providers/descriptors.ts`.
- Modify: `src/features/providers/brandLogos.ts`.
- Create asset: `src/assets/icons/litellm.svg` using LiteLLM's official SVG mark from its public brand assets.
- Modify: `src/features/providers/adapters.ts`.
- Modify: `src/features/providers/useProviderWorkbench.ts`.
- Modify: `src/features/providers/components/ProviderResourceTable.tsx`: add LiteLLM to named multi-key metric/status branches.
- Extend: `tests/litellmProvider.test.ts`.

**Interfaces:**
- `ProviderBrand` gains `'litellm'`.
- Selector gains `{ brand: 'litellm'; name: string; index: number }`.
- `liteLLMToResource(config, index)` returns named multi-key resource with masked first key, model/key/header counts, exact selector, and usage aggregation keyed by `liteLLMUsageProviderKey(config.name)`.
- Workbench invokes `providersApi.getLiteLLMProviders()` during refetch and handles all CRUD/toggle branches.

- [ ] **Step 1: Write failing resource/descriptor tests.**

```ts
import { liteLLMToResource } from '../src/features/providers/adapters';
import { PROVIDER_DESCRIPTORS, PROVIDER_BRAND_ORDER } from '../src/features/providers/descriptors';

test('exposes LiteLLM as named multi-key provider', () => {
  const resource = liteLLMToResource({
    name: 'production', baseUrl: 'http://localhost:4000',
    apiKeyEntries: [{ apiKey: 'secret-key', weight: 1 }],
    models: [{ name: 'openai/gpt-5', alias: 'public-gpt' }],
  }, 0);
  expect(resource.brand).toBe('litellm');
  expect(resource.name).toBe('production');
  expect(resource.apiKey).toBeNull();
  expect(resource.apiKeyEntryCount).toBe(1);
  expect(resource.selector).toEqual({ brand: 'litellm', name: 'production', index: 0 });
  expect(PROVIDER_DESCRIPTORS.litellm.supportsApiKeyEntries).toBe(true);
  expect(PROVIDER_BRAND_ORDER).toContain('litellm');
});
```

- [ ] **Step 2: Run focused test to verify failure.**

```bash
bun test tests/litellmProvider.test.ts
```

Expected: FAIL because brand/adapter/descriptor do not exist.

- [ ] **Step 3: Implement brand and resource.**

Add descriptor values: named, no single API key, disabled/base URL/prefix/models/headers/priority/test model/API-key entries enabled, proxy URL false at provider level, excluded models false, websockets/cloak false, sheet size `lg`. Add LiteLLM logo asset and registry entry. Insert `litellm` near `openaiCompatibility` in brand order.

Implement adapter by copying named multi-key resource behavior, but use LiteLLM raw type and selector. Do not classify it as `openaiCompatibility`; this keeps generic provider separate.

- [ ] **Step 4: Wire snapshot and mutations.**

In `useProviderWorkbench`:

1. Import LiteLLM type/adapter.
2. Fetch `providersApi.getLiteLLMProviders()` in `refetch` and update config value key `litellm`.
3. Add `case 'litellm'` mapping to resources.
4. Add create/update/delete/toggle branches using name/index API methods.
5. Use `buildNamedProviderDeleteQuery` semantics and preserve source index after reload.
6. Add `usageProviderKey` to `LiteLLMProviderConfig` and set it with a UI helper `liteLLMUsageProviderKey(name)`, returning `litellm-${name.trim().toLowerCase()}`. Extend recent usage/status named multi-key branches in `ProvidersWorkbenchPage.tsx`, `ProviderResourceTable.tsx`, and `ResourceDetailView.tsx` so all UI views aggregate buckets by `usageProviderKey`, API key, and root URL; do not use the display/config name alone.

- [ ] **Step 5: Run tests and commit.**

```bash
bun test tests/litellmProvider.test.ts
bun run type-check
rtk git add src/features/providers/types.ts src/features/providers/descriptors.ts src/features/providers/brandLogos.ts src/assets/icons/litellm.svg src/features/providers/adapters.ts src/features/providers/useProviderWorkbench.ts src/features/providers/components/ProviderResourceTable.tsx tests/litellmProvider.test.ts
rtk git commit -m "feat(ui): add LiteLLM provider workbench" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 8: Add LiteLLM form validation, connectivity, and model discovery

**Files:**
- Modify: `src/components/providers/utils.ts`.
- Modify: `src/features/providers/sheets/forms/BaseProviderForm.tsx`.
- Modify: `src/features/providers/sheets/forms/useConnectivityTest.ts`.
- Modify: `src/features/providers/sheets/forms/useModelDiscovery.ts`.
- Extend: `tests/litellmProvider.test.ts`.

**Interfaces:**
- `isValidLiteLLMRootUrl(value: string): boolean`.
- `buildLiteLLMChatCompletionsEndpoint(root: string): string` returns exactly `{root}/v1/chat/completions`.
- `buildLiteLLMModelsEndpoint(root: string): string` returns exactly `{root}/v1/models`.

- [ ] **Step 1: Write failing URL/probe/discovery tests.**

```ts
import { buildLiteLLMChatCompletionsEndpoint, buildLiteLLMModelsEndpoint, isValidLiteLLMRootUrl } from '../src/components/providers/utils';

test('LiteLLM root URL rejects /v1 and appends native paths once', () => {
  expect(isValidLiteLLMRootUrl('http://localhost:4000')).toBe(true);
  expect(isValidLiteLLMRootUrl('http://localhost:4000/')).toBe(true);
  expect(isValidLiteLLMRootUrl('http://localhost:4000/v1')).toBe(false);
  expect(isValidLiteLLMRootUrl('localhost:4000')).toBe(false);
  expect(buildLiteLLMChatCompletionsEndpoint('http://localhost:4000/')).toBe('http://localhost:4000/v1/chat/completions');
  expect(buildLiteLLMModelsEndpoint('http://localhost:4000')).toBe('http://localhost:4000/v1/models');
});
```

- [ ] **Step 2: Run test to verify failure.**

```bash
bun test tests/litellmProvider.test.ts
```

Expected: FAIL because helpers and provider branches do not exist.

- [ ] **Step 3: Implement endpoint helpers and form behavior.**

Use strict `URL` parsing in `isValidLiteLLMRootUrl`; allow only `http:`/`https:`, require hostname, remove trailing slash for comparison, reject pathname `/v1` case-insensitively. Do not auto-prefix missing scheme for LiteLLM.

Add LiteLLM to `BaseProviderForm` initial form and API-key-entry branch. Add LiteLLM-specific root help text below Base URL. Add validation error before generic base URL error when invalid. Keep model editor static aliases and thinking controls; do not show image/excluded/websocket controls.

- [ ] **Step 4: Implement connection test.**

In `useConnectivityTest`, treat `litellm` like named OpenAI key entries but use `buildLiteLLMChatCompletionsEndpoint`. Probe body:

```json
{"model":"<first static model>","messages":[{"role":"user","content":"Hi"}],"stream":false,"max_tokens":5}
```

Use first key entry/auth index, configured headers, bearer placeholder when auth index is used, and existing `/v0/management/api-call` transport. Do not test `/v1/responses` or `/v1/messages` from UI; backend executor covers those native routes.

- [ ] **Step 5: Implement discovery.**

Add `litellm` to `MODEL_DISCOVERY_BRANDS`. Use `modelsApi.fetchV1ModelsViaApiCall(root, key, headers, authIndex)`, which produces `/v1/models`. Do not add a no-auth retry fallback: LiteLLM Proxy `/v1/models` requires the configured key, unlike the public OpenAI-compatible catalogs the existing retry accommodates. `ModelDiscoveryPanel` already requires explicit selection; keep that behavior.

- [ ] **Step 6: Run tests and commit.**

```bash
bun test tests/litellmProvider.test.ts
bun run type-check
rtk git add src/components/providers/utils.ts src/features/providers/sheets/forms/BaseProviderForm.tsx src/features/providers/sheets/forms/useConnectivityTest.ts src/features/providers/sheets/forms/useModelDiscovery.ts tests/litellmProvider.test.ts
rtk git commit -m "feat(ui): add LiteLLM form connectivity and discovery" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 9: Add localization, navigation copy, and browser verification

**Files:**
- Modify: `src/i18n/locales/en.json`.
- Modify: `src/i18n/locales/zh-CN.json`.
- Modify: `src/i18n/locales/zh-TW.json`.
- Modify: `src/i18n/locales/ru.json`.
- Do not modify: `src/features/providers/sheets/ProviderSheet.tsx`; its existing default route description produces `/ai-providers/litellm`.
- Do not modify: `src/router/MainRoutes.tsx`; existing `/ai-providers` route renders all provider brands.
- Extend: `tests/litellmProvider.test.ts` with a translation-key presence check across all four locale files.

**Interfaces:**
- Translation key `providersPage.providerNames.litellm` exists in all locales.
- Translation keys explain LiteLLM Proxy root URL and `/v1` rejection.
- Provider sheet resolves LiteLLM without falling back to OpenAI compatibility route copy.

- [ ] **Step 1: Add localized strings.**

Add equivalent keys in each locale:

```json
"litellm": "LiteLLM Proxy"
```

Under form add root URL help (`providersPage.form.litellmBaseUrlHint`) and validation strings (`providersPage.form.validation.litellmBaseUrlInvalid`). Add `providersPage.providerNames.litellm`. Reuse existing generic connectivity/discovery copy unchanged; LiteLLM introduces no new connectivity/discovery strings. Keep English in English locale; translate into existing locale languages.

- [ ] **Step 2: Verify category/card/sheet rendering.**

Ensure `PROVIDER_LOGOS.litellm` exists, `ProviderCategoryList` uses `providersPage.providerNames.litellm`, table renders named multi-key metrics, and `ProviderSheet` uses route `/ai-providers/litellm` in description.

- [ ] **Step 3: Run full UI verification.**

```bash
cd /Users/MAC/Workspace/Cli-Proxy-API-Management-Center
bun run verify
```

Expected: tests, lint, type-check, and production build PASS.

- [ ] **Step 4: Start app and browser-check.**

Use project preview configuration, open provider workbench, and verify:

1. LiteLLM category/card appears.
2. Create form shows named provider, root URL, multiple API-key rows, static models, headers, priority, cooling, and disabled controls.
3. `/v1` root URL shows validation error; root URL saves after correction.
4. Connection test sends expected endpoint/body through management API.
5. Discovery displays `/v1/models` results and only selected models enter form.
6. Edit leaves existing API-key fields blank/preserved.
7. Delete and enable/disable update card state.
8. Existing OpenAI-compatible card behavior remains unchanged.

- [ ] **Step 5: Commit.**

```bash
rtk git add src/i18n/locales/en.json src/i18n/locales/zh-CN.json src/i18n/locales/zh-TW.json src/i18n/locales/ru.json tests/litellmProvider.test.ts
rtk git commit -m "feat(ui): localize LiteLLM provider" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 10: Add backend route-level integration coverage and complete verification

**Files:**
- Modify: `internal/api/server_test.go` in existing HTTP server integration tests.
- Do not list unspecified test files in this task; `internal/api/server_test.go` provides route-level coverage and earlier tasks cover all unit boundaries.

- [ ] **Step 1: Add route-level native endpoint test.**

Configure an `httptest.Server` as LiteLLM upstream and a CLIProxyAPI server/config with three static aliases in one LiteLLM block. Invoke public `/v1/chat/completions`, `/v1/responses`, and `/v1/messages`. Assert upstream sees matching path, upstream model name, native body fields, bearer key, and native response reaches caller.

For streaming cases, assert exact upstream SSE byte sequence reaches each public caller, including Responses and Anthropic event names.

- [ ] **Step 2: Run all backend tests.**

```bash
rtk gofmt -w internal/config/config_types.go internal/config/config.go internal/config/config_normalization.go internal/config/parse.go internal/config/config_load.go internal/config/weight.go internal/watcher/synthesizer/config.go internal/watcher/clients.go internal/watcher/diff/litellm.go internal/watcher/diff/config_diff.go internal/modelconfig/model_hash.go sdk/config/config.go sdk/cliproxy/auth/conductor_models.go sdk/cliproxy/auth/api_key_model_capabilities.go sdk/cliproxy/service_executors.go sdk/cliproxy/service_models.go internal/runtime/executor/litellm_executor.go internal/api/handlers/management/config_lists.go internal/api/handlers/management/config_auth_index.go internal/api/handlers/management/api_tools.go internal/api/server_management.go
rtk go test ./...
```

Expected: PASS. If failure appears, invoke `superpowers:systematic-debugging` before changing code; do not weaken native passthrough guarantees.

- [ ] **Step 3: Run required compile check.**

```bash
rtk go build -o test-output ./cmd/server && rm test-output
```

Expected: PASS with `test-output` removed.

- [ ] **Step 4: Run complete UI suite again.**

```bash
cd /Users/MAC/Workspace/Cli-Proxy-API-Management-Center
bun run verify
```

Expected: PASS.

- [ ] **Step 5: Cross-repository contract audit.**

Manually compare backend and UI for:

- YAML key `litellm`.
- Management envelope key `litellm`.
- CRUD path `/v0/management/litellm` through API client base path.
- Provider keys `litellm-<lowercase-name>`.
- Selector name/index behavior.
- Auth index placement.
- Kebab/camel field transforms.
- Root URL `/v1` rejection.
- Static alias model fields.
- Existing API-key preservation on blank edit fields.

- [ ] **Step 6: Check unrelated worktree state.**

```bash
cd /Users/MAC/Workspace/CLIProxyAPI
rtk git status --short
```

Expected: only intended LiteLLM commits plus pre-existing `Dockerfile` and `docker-compose.yml` modifications. Do not stage those pre-existing files.

- [ ] **Step 7: Final commit.**

```bash
rtk git add internal/api/server_test.go
rtk git commit -m "test: verify LiteLLM native routes" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Spec coverage checklist

- [ ] Dedicated named LiteLLM provider, isolated from generic OpenAI compatibility.
- [ ] Static aliases and weighted API-key pools.
- [ ] Root URL validation and exactly-once `/v1` path construction.
- [ ] Native Chat Completions forwarding.
- [ ] Native Responses forwarding.
- [ ] Native Anthropic Messages forwarding.
- [ ] Native non-stream/SSE passthrough.
- [ ] Bearer auth, custom headers, per-key proxy transport.
- [ ] Existing payload/thinking rules without cross-protocol translation.
- [ ] Local token counting.
- [ ] Usage accounting for OpenAI/Responses/Claude payloads.
- [ ] Error/status/body propagation and retry/cooling integration.
- [ ] Auth synthesis, executor registration, model registration, alias resolution.
- [ ] Management CRUD and auth indexes.
- [ ] Backend docs and README.
- [ ] Management Center types, transforms, CRUD, card, form, validation, test, discovery.
- [ ] Four locales.
- [ ] Backend tests, UI tests, browser verification, full build checks.
