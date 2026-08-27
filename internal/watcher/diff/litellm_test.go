package diff

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestDiffLiteLLMDoesNotPrintKey(t *testing.T) {
	oldList := []config.LiteLLMProvider{{Name: "prod", BaseURL: "http://one", APIKeyEntries: []config.LiteLLMAPIKey{{APIKey: "secret-old"}}}}
	newList := []config.LiteLLMProvider{{Name: "prod", BaseURL: "http://two", APIKeyEntries: []config.LiteLLMAPIKey{{APIKey: "secret-new"}}}}
	joined := strings.Join(DiffLiteLLM(oldList, newList), "\n")
	if strings.Contains(joined, "secret-old") || strings.Contains(joined, "secret-new") {
		t.Fatalf("diff leaked key: %s", joined)
	}
	if !strings.Contains(joined, "base-url") {
		t.Fatalf("diff omitted base URL change: %s", joined)
	}
}

func TestComputeLiteLLMModelsHashChangesWithModelMetadata(t *testing.T) {
	first := []config.LiteLLMModel{{Name: "upstream", Alias: "public"}}
	second := []config.LiteLLMModel{{Name: "upstream", Alias: "public", DisplayName: "Public"}}
	if ComputeLiteLLMModelsHash(first) == ComputeLiteLLMModelsHash(second) {
		t.Fatal("model metadata change did not change hash")
	}
}

func TestComputeLiteLLMModelsHashIsStableForEquivalentEntries(t *testing.T) {
	first := []config.LiteLLMModel{{Name: " Upstream ", Alias: " Public "}}
	second := []config.LiteLLMModel{{Name: "upstream", Alias: "public"}}
	if ComputeLiteLLMModelsHash(first) != ComputeLiteLLMModelsHash(second) {
		t.Fatal("equivalent model entries produced different hashes")
	}
}
