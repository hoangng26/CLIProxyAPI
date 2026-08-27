package synthesizer

import (
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestSynthesizeLiteLLMKeys(t *testing.T) {
	weight := 2
	cfg := &config.Config{LiteLLM: []config.LiteLLMProvider{{
		Name: "Production", BaseURL: "http://localhost:4000", Priority: 3,
		APIKeyEntries: []config.LiteLLMAPIKey{{APIKey: "key-a", Weight: &weight, ProxyURL: "direct"}, {APIKey: "key-b"}},
		Models:        []config.LiteLLMModel{{Name: "upstream", Alias: "public"}},
	}}}
	auths, err := NewConfigSynthesizer().Synthesize(&SynthesisContext{Config: cfg, Now: time.Now(), IDGenerator: NewStableIDGenerator()})
	if err != nil {
		t.Fatal(err)
	}
	if len(auths) != 2 {
		t.Fatalf("auth count = %d", len(auths))
	}
	if auths[0].Provider != "litellm-production" {
		t.Fatalf("provider = %q", auths[0].Provider)
	}
	if auths[0].Attributes["config_name"] != "Production" || auths[0].Attributes["provider_key"] != "litellm-production" {
		t.Fatalf("attrs = %#v", auths[0].Attributes)
	}
	if auths[0].Attributes["api_key"] != "key-a" || auths[0].ProxyURL != "direct" {
		t.Fatalf("credential attrs = %#v proxy=%q", auths[0].Attributes, auths[0].ProxyURL)
	}
}
