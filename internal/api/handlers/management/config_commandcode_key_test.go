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
		cfg: &config.Config{CommandCodeKey: []config.CommandCodeKey{{
			APIKey:         "cc-key",
			Priority:       1,
			BaseURL:        "https://api.commandcode.ai",
			DisableCooling: false,
		}}},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/commandcode-api-key", strings.NewReader(`{
		"index": 0,
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

func TestPutCommandCodeKeysDefaultsBaseURL(t *testing.T) {
	h := &Handler{
		cfg:            &config.Config{},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/commandcode-api-key", strings.NewReader(`[
		{"api-key": "cc-key-1"},
		{"api-key": ""},
		{"api-key": "cc-key-2", "base-url": "https://custom.example/v1"}
	]`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.PutCommandCodeKeys(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(h.cfg.CommandCodeKey) != 2 {
		t.Fatalf("len(CommandCodeKey) = %d, want 2", len(h.cfg.CommandCodeKey))
	}
	if h.cfg.CommandCodeKey[0].APIKey != "cc-key-1" {
		t.Fatalf("api-key[0] = %q, want cc-key-1", h.cfg.CommandCodeKey[0].APIKey)
	}
	if h.cfg.CommandCodeKey[0].BaseURL != "https://api.commandcode.ai" {
		t.Fatalf("base-url[0] = %q, want default https://api.commandcode.ai", h.cfg.CommandCodeKey[0].BaseURL)
	}
	if h.cfg.CommandCodeKey[1].APIKey != "cc-key-2" {
		t.Fatalf("api-key[1] = %q, want cc-key-2", h.cfg.CommandCodeKey[1].APIKey)
	}
	if h.cfg.CommandCodeKey[1].BaseURL != "https://custom.example/v1" {
		t.Fatalf("base-url[1] = %q, want https://custom.example/v1", h.cfg.CommandCodeKey[1].BaseURL)
	}
}

func TestDeleteCommandCodeKeyByIndex(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{CommandCodeKey: []config.CommandCodeKey{
			{APIKey: "cc-a", BaseURL: "https://api.commandcode.ai"},
			{APIKey: "cc-b", BaseURL: "https://api.commandcode.ai"},
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
	if h.cfg.CommandCodeKey[0].APIKey != "cc-b" {
		t.Fatalf("remaining api-key = %q, want cc-b", h.cfg.CommandCodeKey[0].APIKey)
	}
}
