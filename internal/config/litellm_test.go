package config

import (
	"strings"
	"testing"
)

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
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.LiteLLM) != 1 || cfg.LiteLLM[0].Name != "production" {
		t.Fatalf("unexpected providers: %+v", cfg.LiteLLM)
	}
	if cfg.LiteLLM[0].BaseURL != "http://localhost:4000" {
		t.Fatalf("base URL = %q", cfg.LiteLLM[0].BaseURL)
	}
}

func TestParseConfigBytesRejectsLiteLLMBaseURLWithV1(t *testing.T) {
	_, err := ParseConfigBytes([]byte(`
litellm:
  - name: production
    base-url: http://localhost:4000/v1
    api-key-entries: [{api-key: key}]
`))
	if err == nil || !strings.Contains(err.Error(), "base-url") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseConfigBytesAcceptsLiteLLMAliasPool(t *testing.T) {
	aliasPool := []byte(`litellm:
  - name: prod
    base-url: http://one
    api-key-entries: [{api-key: a}]
    models: [{name: one, alias: same}, {name: two, alias: same}, {name: one, alias: same}]
`)
	cfg, err := ParseConfigBytes(aliasPool)
	if err != nil || len(cfg.LiteLLM[0].Models) != 2 {
		t.Fatalf("alias pool rejected or not deduplicated: cfg=%+v err=%v", cfg, err)
	}
}

func TestSanitizeLiteLLMKeepsDisabledKeylessAndDropsEmptyEnabled(t *testing.T) {
	cfg := &Config{LiteLLM: []LiteLLMProvider{
		{Name: " disabled ", BaseURL: " https://one/ ", Disabled: true},
		{Name: "empty", BaseURL: "https://two"},
		{Name: "ready", BaseURL: "https://three", APIKeyEntries: []LiteLLMAPIKey{{APIKey: " key "}}},
	}}
	cfg.SanitizeLiteLLM()
	if len(cfg.LiteLLM) != 2 || cfg.LiteLLM[0].Name != "disabled" || cfg.LiteLLM[1].Name != "ready" {
		t.Fatalf("providers = %+v", cfg.LiteLLM)
	}
	if cfg.LiteLLM[0].BaseURL != "https://one" || cfg.LiteLLM[1].APIKeyEntries[0].APIKey != "key" {
		t.Fatalf("normalization = %+v", cfg.LiteLLM)
	}
}

func TestValidateLiteLLMProvidersRejectsDuplicateNamesAndInvalidURL(t *testing.T) {
	cfg := &Config{LiteLLM: []LiteLLMProvider{
		{Name: "Prod", BaseURL: "https://one", APIKeyEntries: []LiteLLMAPIKey{{APIKey: "a"}}},
		{Name: "prod", BaseURL: "localhost:4000/v1", APIKeyEntries: []LiteLLMAPIKey{{APIKey: "b"}}},
	}}
	if err := cfg.ValidateLiteLLMProviders(); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("error = %v", err)
	}
	cfg.LiteLLM = []LiteLLMProvider{{Name: "prod", BaseURL: "localhost:4000", APIKeyEntries: []LiteLLMAPIKey{{APIKey: "a"}}}}
	if err := cfg.ValidateLiteLLMProviders(); err == nil || !strings.Contains(err.Error(), "base-url") {
		t.Fatalf("error = %v", err)
	}
}

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
	if cfg.LiteLLM[0].Headers["X-Test"] != "one" || cfg.LiteLLM[0].APIKeyEntries[0].APIKey != "key" || cfg.LiteLLM[0].Models[0].Alias != "public" {
		t.Fatal("clone shares nested LiteLLM state")
	}
}
