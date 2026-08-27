package cliproxy

import (
	"testing"

	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestRegisterExecutorForAuth_LiteLLMUsesNamedExecutor(t *testing.T) {
	service := &Service{cfg: &config.Config{}, coreManager: coreauth.NewManager(nil, nil, nil)}
	auth := &coreauth.Auth{ID: "a", Provider: "litellm-prod", Attributes: map[string]string{"provider_key": "litellm-prod"}}
	service.registerExecutorForAuth(auth, true)
	resolved, ok := service.coreManager.Executor("litellm-prod")
	if !ok {
		t.Fatal("expected LiteLLM executor")
	}
	if _, ok := resolved.(*runtimeexecutor.LiteLLMExecutor); !ok {
		t.Fatalf("executor type = %T, want *executor.LiteLLMExecutor", resolved)
	}
}

func TestRegisterExecutorForAuth_LiteLLMDoesNotUseOpenAICompatExecutor(t *testing.T) {
	service := &Service{cfg: &config.Config{}, coreManager: coreauth.NewManager(nil, nil, nil)}
	auth := &coreauth.Auth{ID: "a", Provider: "litellm-prod"}
	service.registerExecutorForAuth(auth, true)
	resolved, _ := service.coreManager.Executor("litellm-prod")
	if _, ok := resolved.(*runtimeexecutor.OpenAICompatExecutor); ok {
		t.Fatal("LiteLLM registered generic OpenAI compatibility executor")
	}
}

func TestLiteLLMModelsRegisterUnderNamedProvider(t *testing.T) {
	service := &Service{cfg: &config.Config{LiteLLM: []config.LiteLLMProvider{{
		Name: "prod", Models: []config.LiteLLMModel{{Name: "openai/gpt-5", Alias: "public-gpt"}},
	}}}, coreManager: coreauth.NewManager(nil, nil, nil)}
	auth := &coreauth.Auth{ID: "a", Provider: "litellm-prod", Attributes: map[string]string{"auth_kind": coreauth.AuthKindAPIKey, "api_key": "key", "config_index": "0", "provider_key": "litellm-prod"}}
	service.registerModelsForAuth(nil, auth)
	providers := GlobalModelRegistry().GetAvailableModelsByProvider("litellm-prod")
	for _, model := range providers {
		if model != nil && model.ID == "public-gpt" {
			return
		}
	}
	t.Fatalf("models = %v, want public-gpt", providers)
}

var _ = runtimeexecutor.LiteLLMExecutor{}

// end
