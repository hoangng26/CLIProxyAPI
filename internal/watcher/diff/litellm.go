package diff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/modelconfig"
)

// ComputeLiteLLMModelsHash returns a stable hash for LiteLLM model mappings.
func ComputeLiteLLMModelsHash(models []config.LiteLLMModel) string {
	return modelconfig.ComputeLiteLLMModelsHash(models)
}

// DiffLiteLLM produces human-readable change descriptions without exposing API keys.
func DiffLiteLLM(oldList, newList []config.LiteLLMProvider) []string {
	oldMap := make(map[string]config.LiteLLMProvider, len(oldList))
	newMap := make(map[string]config.LiteLLMProvider, len(newList))
	for _, entry := range oldList {
		oldMap[strings.ToLower(strings.TrimSpace(entry.Name))] = entry
	}
	for _, entry := range newList {
		newMap[strings.ToLower(strings.TrimSpace(entry.Name))] = entry
	}
	keys := make(map[string]struct{}, len(oldMap)+len(newMap))
	for key := range oldMap {
		keys[key] = struct{}{}
	}
	for key := range newMap {
		keys[key] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	changes := make([]string, 0)
	for _, key := range ordered {
		oldEntry, oldOK := oldMap[key]
		newEntry, newOK := newMap[key]
		switch {
		case !oldOK:
			changes = append(changes, fmt.Sprintf("provider added: %s (api-keys=%d, models=%d)", newEntry.Name, len(newEntry.APIKeyEntries), len(newEntry.Models)))
		case !newOK:
			changes = append(changes, fmt.Sprintf("provider removed: %s (api-keys=%d, models=%d)", oldEntry.Name, len(oldEntry.APIKeyEntries), len(oldEntry.Models)))
		default:
			details := make([]string, 0, 5)
			if oldEntry.BaseURL != newEntry.BaseURL {
				details = append(details, "base-url updated")
			}
			if oldEntry.Disabled != newEntry.Disabled {
				details = append(details, fmt.Sprintf("disabled %t -> %t", oldEntry.Disabled, newEntry.Disabled))
			}
			if len(oldEntry.APIKeyEntries) != len(newEntry.APIKeyEntries) {
				details = append(details, fmt.Sprintf("api-keys %d -> %d", len(oldEntry.APIKeyEntries), len(newEntry.APIKeyEntries)))
			}
			if ComputeLiteLLMModelsHash(oldEntry.Models) != ComputeLiteLLMModelsHash(newEntry.Models) {
				details = append(details, fmt.Sprintf("models %d -> %d", len(oldEntry.Models), len(newEntry.Models)))
			}
			if !equalStringMap(oldEntry.Headers, newEntry.Headers) {
				details = append(details, "headers updated")
			}
			if len(details) > 0 {
				changes = append(changes, fmt.Sprintf("provider updated: %s (%s)", newEntry.Name, strings.Join(details, ", ")))
			}
		}
	}
	return changes
}
