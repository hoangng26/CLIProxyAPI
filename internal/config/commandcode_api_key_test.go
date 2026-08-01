package config

import "testing"

func TestParseConfigBytesCommandCodeMultiKey(t *testing.T) {
	raw := []byte(`
commandcode-api-key:
  - name: "primary"
    base-url: "https://api.commandcode.ai"
    api-key-entries:
      - api-key: "user_a"
      - api-key: "user_b"
        weight: 2
    models:
      - name: "deepseek/deepseek-v4-flash"
        alias: "ds-flash"
`)
	cfg, err := ParseConfigBytes(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.CommandCodeKey) != 1 {
		t.Fatalf("blocks = %d", len(cfg.CommandCodeKey))
	}
	block := cfg.CommandCodeKey[0]
	if block.Name != "primary" {
		t.Fatalf("name = %q", block.Name)
	}
	if len(block.APIKeyEntries) != 2 {
		t.Fatalf("entries = %d", len(block.APIKeyEntries))
	}
	if block.APIKeyEntries[0].APIKey != "user_a" {
		t.Fatalf("key0 = %q", block.APIKeyEntries[0].APIKey)
	}
	if block.APIKeyEntries[1].Weight == nil || *block.APIKeyEntries[1].Weight != 2 {
		t.Fatalf("weight1 = %v", block.APIKeyEntries[1].Weight)
	}
	if len(block.Models) != 1 || block.Models[0].Alias != "ds-flash" {
		t.Fatalf("models = %+v", block.Models)
	}
}

func TestParseConfigBytesCommandCodeLegacySingleKeyRejected(t *testing.T) {
	raw := []byte(`
commandcode-api-key:
  - api-key: "user_testkey"
    base-url: "https://api.commandcode.ai"
`)
	_, err := ParseConfigBytes(raw)
	if err == nil {
		t.Fatal("expected legacy shape error")
	}
}

func TestSanitizeCommandCodeKeysDefaultBaseURLAndDropEmptyKeys(t *testing.T) {
	w := 3
	cfg := &Config{
		CommandCodeKey: []CommandCodeProvider{{
			Name: "primary",
			APIKeyEntries: []CommandCodeAPIKey{
				{APIKey: "  "},
				{APIKey: "user_ok", Weight: &w},
			},
		}},
	}
	cfg.SanitizeCommandCodeKeys()
	if len(cfg.CommandCodeKey) != 1 {
		t.Fatalf("blocks = %d", len(cfg.CommandCodeKey))
	}
	if cfg.CommandCodeKey[0].BaseURL != "https://api.commandcode.ai" {
		t.Fatalf("base-url = %q", cfg.CommandCodeKey[0].BaseURL)
	}
	if len(cfg.CommandCodeKey[0].APIKeyEntries) != 1 || cfg.CommandCodeKey[0].APIKeyEntries[0].APIKey != "user_ok" {
		t.Fatalf("entries = %+v", cfg.CommandCodeKey[0].APIKeyEntries)
	}
}

func TestSanitizeCommandCodeKeysDropsEnabledBlockWithNoKeys(t *testing.T) {
	cfg := &Config{
		CommandCodeKey: []CommandCodeProvider{
			{Name: "empty", APIKeyEntries: nil},
			{Name: "ok", APIKeyEntries: []CommandCodeAPIKey{{APIKey: "user_x"}}},
		},
	}
	cfg.SanitizeCommandCodeKeys()
	if len(cfg.CommandCodeKey) != 1 || cfg.CommandCodeKey[0].Name != "ok" {
		t.Fatalf("got %+v", cfg.CommandCodeKey)
	}
}

func TestValidateCommandCodeProvidersDuplicateName(t *testing.T) {
	cfg := &Config{
		CommandCodeKey: []CommandCodeProvider{
			{Name: "primary", APIKeyEntries: []CommandCodeAPIKey{{APIKey: "a"}}},
			{Name: "Primary", APIKeyEntries: []CommandCodeAPIKey{{APIKey: "b"}}},
		},
	}
	if err := cfg.ValidateCommandCodeProviders(); err == nil {
		t.Fatal("expected duplicate name error")
	}
}
