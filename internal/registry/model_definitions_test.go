package registry

import "testing"

func TestModelOverrideHeadersFromEmbeddedModels(t *testing.T) {
	const wantUA = "codex-tui/0.144.0 (Mac OS 26.5.1; arm64) iTerm.app/3.6.11 (codex-tui; 0.144.0)"
	got := ModelOverrideHeaders("gpt-5.6-luna")
	if got == nil {
		t.Fatal("ModelOverrideHeaders(gpt-5.6-luna) = nil, want headers")
	}
	if got["user-agent"] != wantUA {
		t.Fatalf("user-agent = %q, want %q", got["user-agent"], wantUA)
	}
	if got := ModelOverrideHeaders("gpt-5.4"); got != nil {
		t.Fatalf("ModelOverrideHeaders(gpt-5.4) = %#v, want nil", got)
	}
}

func TestGeminiVertexModelsUseFlashLiteReleaseID(t *testing.T) {
	const releaseID = "gemini-3.1-flash-lite"
	const previewID = releaseID + "-preview"

	for _, model := range GetGeminiVertexModels() {
		if model == nil {
			continue
		}
		if model.ID == previewID {
			t.Fatalf("Vertex model ID = %q, want release ID %q", model.ID, releaseID)
		}
		if model.ID == releaseID {
			return
		}
	}

	t.Fatalf("Vertex models do not contain %q", releaseID)
}

func TestWithXAIBuiltinsIncludesVideoPreviewModel(t *testing.T) {
	models := WithXAIBuiltins(nil)

	for _, model := range models {
		if model == nil {
			continue
		}
		if model.ID == xaiBuiltinVideo15PreviewModelID {
			return
		}
	}

	t.Fatalf("expected xAI builtin model %s", xaiBuiltinVideo15PreviewModelID)
}

func TestGetCommandCodeModelsIncludesDeepSeek(t *testing.T) {
	models := GetCommandCodeModels()
	found := false
	for _, m := range models {
		if m != nil && m.ID == "deepseek/deepseek-v4-pro" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing deepseek/deepseek-v4-pro")
	}
}

func TestGetCommandCodeModelsCatalog(t *testing.T) {
	wantIDs := []string{
		"deepseek/deepseek-v4-pro",
		"deepseek/deepseek-v4-flash",
		"moonshotai/Kimi-K2.6",
		"moonshotai/Kimi-K2.5",
		"zai-org/GLM-5.1",
		"zai-org/GLM-5",
		"MiniMaxAI/MiniMax-M2.7",
		"MiniMaxAI/MiniMax-M2.5",
		"Qwen/Qwen3.6-Max-Preview",
		"Qwen/Qwen3.6-Plus",
		"stepfun/Step-3.5-Flash",
	}
	models := GetCommandCodeModels()
	if len(models) != len(wantIDs) {
		t.Fatalf("model count = %d, want %d", len(models), len(wantIDs))
	}
	got := make(map[string]*ModelInfo, len(models))
	for _, m := range models {
		if m == nil {
			t.Fatal("nil model in catalog")
		}
		if m.Object != "model" {
			t.Fatalf("ID %q Object = %q, want model", m.ID, m.Object)
		}
		if m.OwnedBy != "commandcode" {
			t.Fatalf("ID %q OwnedBy = %q, want commandcode", m.ID, m.OwnedBy)
		}
		if m.Type != "commandcode" {
			t.Fatalf("ID %q Type = %q, want commandcode", m.ID, m.Type)
		}
		if m.DisplayName == "" {
			t.Fatalf("ID %q missing DisplayName", m.ID)
		}
		got[m.ID] = m
	}
	for _, id := range wantIDs {
		if _, ok := got[id]; !ok {
			t.Errorf("missing model %q", id)
		}
	}
}

func TestAntigravityWebSearchModelForRequiresRequestedModelCapability(t *testing.T) {
	registryRef := GetGlobalRegistry()
	registryRef.RegisterClient("test-antigravity-websearch-route", "antigravity", []*ModelInfo{
		{ID: "gemini-route-test"},
		{ID: "gemini-web-search-test", SupportsWebSearch: true},
	})
	registryRef.RegisterClient("test-gemini-websearch-route", "gemini", []*ModelInfo{
		{ID: "gemini-cross-provider-route"},
		{ID: "gemini-cross-provider-search", SupportsWebSearch: true},
	})
	t.Cleanup(func() {
		registryRef.UnregisterClient("test-antigravity-websearch-route")
		registryRef.UnregisterClient("test-gemini-websearch-route")
	})

	if got := AntigravityWebSearchModelFor("gemini-route-test"); got != "" {
		t.Fatalf("route model without web search support should not get fallback model, got %q", got)
	}
	if got := AntigravityWebSearchModelFor("gemini-route-test(high)"); got != "" {
		t.Fatalf("suffix route model without web search support should not get fallback model, got %q", got)
	}
	if got := AntigravityWebSearchModelFor("gemini-web-search-test"); got != "gemini-web-search-test" {
		t.Fatalf("AntigravityWebSearchModelFor capable model = %q, want itself", got)
	}
	if got := AntigravityWebSearchModelFor("gemini-cross-provider-route"); got != "" {
		t.Fatalf("cross-provider model should not get Antigravity web search model, got %q", got)
	}
	if got := AntigravityWebSearchModelFor("unknown-model"); got != "" {
		t.Fatalf("unknown model should not get Antigravity web search model, got %q", got)
	}
}
