package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestPatchCommandCodeKeyUpdatesExecutionFields(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{CommandCodeKey: []config.CommandCodeProvider{{
			Name:     "primary",
			Priority: 1,
			BaseURL:  "https://api.commandcode.ai",
			APIKeyEntries: []config.CommandCodeAPIKey{
				{APIKey: "cc-key"},
			},
			DisableCooling: false,
		}}},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/commandcode-api-key", strings.NewReader(`{
		"name": "primary",
		"value": {
			"priority": 7,
			"disable-cooling": true
		}
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.PatchCommandCodeKey(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	entry := h.cfg.CommandCodeKey[0]
	if entry.Priority != 7 {
		t.Fatalf("priority = %d, want 7", entry.Priority)
	}
	if !entry.DisableCooling {
		t.Fatal("disable-cooling = false, want true")
	}
}

func TestPutCommandCodeKeysMultiKeyDefaultsBaseURL(t *testing.T) {
	h := &Handler{
		cfg:            &config.Config{},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/commandcode-api-key", strings.NewReader(`[
		{
			"name": "primary",
			"api-key-entries": [
				{"api-key": "cc-key-1"},
				{"api-key": "cc-key-2", "proxy-url": "http://proxy.local"}
			]
		},
		{
			"name": "secondary",
			"base-url": "https://custom.example/v1",
			"api-key-entries": [{"api-key": "cc-key-3"}]
		}
	]`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.PutCommandCodeKeys(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(h.cfg.CommandCodeKey) != 2 {
		t.Fatalf("len(CommandCodeKey) = %d, want 2", len(h.cfg.CommandCodeKey))
	}
	if h.cfg.CommandCodeKey[0].Name != "primary" {
		t.Fatalf("name[0] = %q, want primary", h.cfg.CommandCodeKey[0].Name)
	}
	if h.cfg.CommandCodeKey[0].BaseURL != "https://api.commandcode.ai" {
		t.Fatalf("base-url[0] = %q, want default", h.cfg.CommandCodeKey[0].BaseURL)
	}
	if len(h.cfg.CommandCodeKey[0].APIKeyEntries) != 2 {
		t.Fatalf("entries[0] = %d, want 2", len(h.cfg.CommandCodeKey[0].APIKeyEntries))
	}
	if h.cfg.CommandCodeKey[1].BaseURL != "https://custom.example/v1" {
		t.Fatalf("base-url[1] = %q", h.cfg.CommandCodeKey[1].BaseURL)
	}
}

func TestPutCommandCodeKeysRejectsLegacyShape(t *testing.T) {
	h := &Handler{
		cfg:            &config.Config{},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/commandcode-api-key", strings.NewReader(`[
		{"api-key": "cc-key-1"}
	]`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.PutCommandCodeKeys(ctx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestDeleteCommandCodeKeyByName(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{CommandCodeKey: []config.CommandCodeProvider{
			{Name: "a", BaseURL: "https://api.commandcode.ai", APIKeyEntries: []config.CommandCodeAPIKey{{APIKey: "cc-a"}}},
			{Name: "b", BaseURL: "https://api.commandcode.ai", APIKeyEntries: []config.CommandCodeAPIKey{{APIKey: "cc-b"}}},
		}},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/commandcode-api-key?name=a", nil)

	h.DeleteCommandCodeKey(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(h.cfg.CommandCodeKey) != 1 {
		t.Fatalf("len(CommandCodeKey) = %d, want 1", len(h.cfg.CommandCodeKey))
	}
	if h.cfg.CommandCodeKey[0].Name != "b" {
		t.Fatalf("remaining name = %q, want b", h.cfg.CommandCodeKey[0].Name)
	}
}

func TestDeleteCommandCodeKeyByIndex(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{CommandCodeKey: []config.CommandCodeProvider{
			{Name: "a", BaseURL: "https://api.commandcode.ai", APIKeyEntries: []config.CommandCodeAPIKey{{APIKey: "cc-a"}}},
			{Name: "b", BaseURL: "https://api.commandcode.ai", APIKeyEntries: []config.CommandCodeAPIKey{{APIKey: "cc-b"}}},
		}},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/commandcode-api-key?index=0", nil)

	h.DeleteCommandCodeKey(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(h.cfg.CommandCodeKey) != 1 {
		t.Fatalf("len(CommandCodeKey) = %d, want 1", len(h.cfg.CommandCodeKey))
	}
	if h.cfg.CommandCodeKey[0].Name != "b" {
		t.Fatalf("remaining name = %q, want b", h.cfg.CommandCodeKey[0].Name)
	}
}
