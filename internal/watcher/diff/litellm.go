package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// ComputeLiteLLMModelsHash returns a stable hash for LiteLLM model mappings.
func ComputeLiteLLMModelsHash(models []config.LiteLLMModel) string {
	if len(models) == 0 {
		return ""
	}
	data, err := json.Marshal(models)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// DiffLiteLLM produces human-readable changes without exposing API keys.
func DiffLiteLLM(oldList, newList []config.LiteLLMProvider) []string {
	changes := make([]string, 0)
	oldByName := make(map[string]config.LiteLLMProvider, len(oldList))
	newByName := make(map[string]config.LiteLLMProvider, len(newList))
	for _, provider := range oldList {
		oldByName[strings.ToLower(strings.TrimSpace(provider.Name))] = provider
	}
	for _, provider := range newList {
		newByName[strings.ToLower(strings.TrimSpace(provider.Name))] = provider
	}
	for _, provider := range oldList {
		key := strings.ToLower(strings.TrimSpace(provider.Name))
		if _, ok := newByName[key]; !ok {
			changes = append(changes, fmt.Sprintf("removed %s", provider.Name))
		}
	}
	for _, provider := range newList {
		key := strings.ToLower(strings.TrimSpace(provider.Name))
		old, ok := oldByName[key]
		if !ok {
			changes = append(changes, fmt.Sprintf("added %s", provider.Name))
			continue
		}
		if old.BaseURL != provider.BaseURL {
			changes = append(changes, fmt.Sprintf("%s base-url changed", provider.Name))
		}
		if old.Disabled != provider.Disabled {
			changes = append(changes, fmt.Sprintf("%s disabled: %t -> %t", provider.Name, old.Disabled, provider.Disabled))
		}
		if ComputeLiteLLMModelsHash(old.Models) != ComputeLiteLLMModelsHash(provider.Models) {
			changes = append(changes, fmt.Sprintf("%s models changed", provider.Name))
		}
		if len(old.APIKeyEntries) != len(provider.APIKeyEntries) {
			changes = append(changes, fmt.Sprintf("%s API key count: %d -> %d", provider.Name, len(old.APIKeyEntries), len(provider.APIKeyEntries)))
		}
	}
	return changes
}
