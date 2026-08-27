package auth

import (
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
)

func TestLiteLLMAliasResolvesToUpstreamModel(t *testing.T) {
	cfg := &internalconfig.Config{LiteLLM: []internalconfig.LiteLLMProvider{{
		Name: "prod", BaseURL: "http://localhost:4000",
		APIKeyEntries: []internalconfig.LiteLLMAPIKey{{APIKey: "key"}},
		Models:        []internalconfig.LiteLLMModel{{Name: "openai/gpt-5", Alias: "public-gpt"}},
	}}}
	auth := &Auth{Provider: "litellm-prod", Attributes: map[string]string{
		"api_key": "key", "base_url": "http://localhost:4000", "config_name": "prod", "config_index": "0", "provider_key": "litellm-prod",
	}}
	if got := resolveUpstreamModelForLiteLLM(cfg, auth, "public-gpt"); got != "openai/gpt-5" {
		t.Fatalf("model = %q", got)
	}
}

func TestLiteLLMProviderKeyIsIndependentPerNamedInstance(t *testing.T) {
	if got := util.LiteLLMProviderKey("Production"); got != "litellm-production" {
		t.Fatalf("key = %q", got)
	}
	if got := util.LiteLLMProviderKey("staging"); got == "litellm-production" {
		t.Fatalf("keys collide")
	}
}

func TestLiteLLMResolverHandlesMissingAlias(t *testing.T) {
	cfg := &internalconfig.Config{LiteLLM: []internalconfig.LiteLLMProvider{{Name: "prod", Models: []internalconfig.LiteLLMModel{{Name: "openai/gpt-5"}}}}}
	auth := &Auth{Provider: "litellm-prod", Attributes: map[string]string{"auth_kind": AuthKindAPIKey}}
	if got := resolveUpstreamModelForLiteLLM(cfg, auth, "openai/gpt-5"); got != "openai/gpt-5" {
		t.Fatalf("model = %q", got)
	}
}

func TestCompileLiteLLMModelCapabilities(t *testing.T) {
	cfg := &internalconfig.Config{LiteLLM: []internalconfig.LiteLLMProvider{{
		Name: "prod", Models: []internalconfig.LiteLLMModel{{Name: "openai/gpt-5", Alias: "public-gpt"}},
	}}}
	auth := &Auth{ID: "auth-1", Provider: "litellm-prod", Attributes: map[string]string{"auth_kind": AuthKindAPIKey, "config_index": "0", "api_key": "key"}}
	routes := compileAPIKeyModelCapabilitiesForAuth(cfg, auth)
	if len(routes["public-gpt"]) != 1 || routes["public-gpt"][0].upstreamModel != "openai/gpt-5" {
		t.Fatalf("routes = %#v", routes)
	}
}

func TestCompileLiteLLMModelCapabilitiesKeepsSeparateUpstreamsForAliasPool(t *testing.T) {
	cfg := &internalconfig.Config{LiteLLM: []internalconfig.LiteLLMProvider{{
		Name: "prod", Models: []internalconfig.LiteLLMModel{{Name: "one", Alias: "same"}, {Name: "two", Alias: "same"}},
	}}}
	auth := &Auth{ID: "auth-1", Provider: "litellm-prod", Attributes: map[string]string{"auth_kind": AuthKindAPIKey, "config_index": "0", "api_key": "key"}}
	routes := compileAPIKeyModelCapabilitiesForAuth(cfg, auth)
	if len(routes["same"]) != 2 {
		t.Fatalf("routes = %#v", routes["same"])
	}
}

var _ = internalconfig.LiteLLMProvider{}
var _ = util.LiteLLMProviderKey

// end
