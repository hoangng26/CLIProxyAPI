package registry

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

const commandCodeModelsURL = "https://api.commandcode.ai/provider/v1/models"

type commandCodeModelsStore struct {
	mu   sync.RWMutex
	data []*ModelInfo
}

var commandCodeCatalogStore = &commandCodeModelsStore{}

func init() {
	if err := loadCommandCodeModels(commandCodeBuiltinModels(), "builtin"); err != nil {
		log.Warnf("registry: failed to load builtin CommandCode models: %v", err)
	}
}

// GetCommandCodeModels returns the current CommandCode model catalog (remote-refreshed when available).
func GetCommandCodeModels() []*ModelInfo {
	commandCodeCatalogStore.mu.RLock()
	defer commandCodeCatalogStore.mu.RUnlock()
	if len(commandCodeCatalogStore.data) == 0 {
		return cloneModelInfos(commandCodeBuiltinModels())
	}
	return cloneModelInfos(commandCodeCatalogStore.data)
}

func loadCommandCodeModels(models []*ModelInfo, source string) error {
	if len(models) == 0 {
		return fmt.Errorf("%s: CommandCode model catalog is empty", source)
	}
	cloned := cloneModelInfos(models)
	commandCodeCatalogStore.mu.Lock()
	defer commandCodeCatalogStore.mu.Unlock()
	commandCodeCatalogStore.data = cloned
	return nil
}

func commandCodeModelsEqual(a, b []*ModelInfo) bool {
	if len(a) != len(b) {
		return false
	}
	aj, errA := json.Marshal(a)
	bj, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(aj) == string(bj)
}

type commandCodeRemoteModelsPayload struct {
	Object string                        `json:"object"`
	Data   []commandCodeRemoteModelEntry `json:"data"`
}

type commandCodeRemoteModelEntry struct {
	ID            string `json:"id"`
	Object        string `json:"object"`
	Created       int64  `json:"created"`
	OwnedBy       string `json:"owned_by"`
	Name          string `json:"name"`
	ContextLength int    `json:"context_length"`
}

func parseCommandCodeRemoteModels(data []byte) ([]*ModelInfo, error) {
	var payload commandCodeRemoteModelsPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode CommandCode models: %w", err)
	}
	if len(payload.Data) == 0 {
		return nil, fmt.Errorf("CommandCode models payload has no data")
	}
	out := make([]*ModelInfo, 0, len(payload.Data))
	seen := make(map[string]struct{}, len(payload.Data))
	for i, entry := range payload.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			return nil, fmt.Errorf("CommandCode models data[%d]: id is required", i)
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		displayName := strings.TrimSpace(entry.Name)
		if displayName == "" {
			displayName = id
		}
		object := strings.TrimSpace(entry.Object)
		if object == "" {
			object = "model"
		}
		out = append(out, &ModelInfo{
			ID:              id,
			Object:          object,
			Created:         entry.Created,
			OwnedBy:         "commandcode",
			Type:            "commandcode",
			DisplayName:     displayName,
			Name:            id,
			ContextLength:   entry.ContextLength,
			InputTokenLimit: entry.ContextLength,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("CommandCode models payload produced no valid models")
	}
	return out, nil
}

func commandCodeBuiltinModels() []*ModelInfo {
	// Fallback catalog used before the first successful remote refresh.
	type pair struct{ id, name string }
	pairs := []pair{
		{"deepseek/deepseek-v4-pro", "DeepSeek V4 Pro"},
		{"deepseek/deepseek-v4-flash", "DeepSeek V4 Flash"},
		{"xiaomi/mimo-v2.5", "Mimo V2.5"},
		{"xiaomi/mimo-v2.5-pro", "Mimo V2.5 Pro"},
		{"moonshotai/Kimi-K2.6", "Kimi K2.6"},
		{"moonshotai/Kimi-K2.5", "Kimi K2.5"},
		{"zai-org/GLM-5.1", "GLM 5.1"},
		{"zai-org/GLM-5", "GLM 5"},
		{"MiniMaxAI/MiniMax-M2.7", "MiniMax M2.7"},
		{"MiniMaxAI/MiniMax-M2.5", "MiniMax M2.5"},
		{"Qwen/Qwen3.6-Max-Preview", "Qwen 3.6 Max Preview"},
		{"Qwen/Qwen3.6-Plus", "Qwen 3.6 Plus"},
		{"stepfun/Step-3.5-Flash", "Step 3.5 Flash"},
	}
	out := make([]*ModelInfo, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, &ModelInfo{
			ID:          p.id,
			Object:      "model",
			DisplayName: p.name,
			OwnedBy:     "commandcode",
			Type:        "commandcode",
			Name:        p.id,
		})
	}
	return out
}
