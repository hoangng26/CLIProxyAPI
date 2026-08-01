package config

import "testing"

func TestParseConfigBytesCommandCodeAPIKey(t *testing.T) {
	raw := []byte(`
commandcode-api-key:
  - api-key: "user_testkey"
    base-url: "https://api.commandcode.ai"
    models:
      - name: "deepseek/deepseek-v4-flash"
        alias: "ds-flash"
`)
	cfg, err := ParseConfigBytes(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.CommandCodeKey) != 1 {
		t.Fatalf("got %d keys", len(cfg.CommandCodeKey))
	}
	if cfg.CommandCodeKey[0].APIKey != "user_testkey" {
		t.Fatalf("api-key = %q", cfg.CommandCodeKey[0].APIKey)
	}
	if len(cfg.CommandCodeKey[0].Models) != 1 || cfg.CommandCodeKey[0].Models[0].Alias != "ds-flash" {
		t.Fatalf("models = %+v", cfg.CommandCodeKey[0].Models)
	}
}

func TestSanitizeCommandCodeKeysDefaultBaseURL(t *testing.T) {
	cfg := &Config{
		CommandCodeKey: []CommandCodeKey{{APIKey: "user_x"}},
	}
	cfg.SanitizeCommandCodeKeys()
	if len(cfg.CommandCodeKey) != 1 {
		t.Fatalf("expected 1 key, got %d", len(cfg.CommandCodeKey))
	}
	if cfg.CommandCodeKey[0].BaseURL != "https://api.commandcode.ai" {
		t.Fatalf("base-url = %q", cfg.CommandCodeKey[0].BaseURL)
	}
}

func TestSanitizeCommandCodeKeysDropsEmptyAPIKey(t *testing.T) {
	cfg := &Config{
		CommandCodeKey: []CommandCodeKey{{APIKey: "  "}, {APIKey: "user_ok"}},
	}
	cfg.SanitizeCommandCodeKeys()
	if len(cfg.CommandCodeKey) != 1 || cfg.CommandCodeKey[0].APIKey != "user_ok" {
		t.Fatalf("got %+v", cfg.CommandCodeKey)
	}
}
