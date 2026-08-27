package management

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher/synthesizer"
)

type liteLLMAPIKeyWithAuthIndex struct {
	config.LiteLLMAPIKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type liteLLMProviderWithAuthIndex struct {
	Name                string                          `json:"name"`
	Priority            int                             `json:"priority,omitempty"`
	Disabled            bool                            `json:"disabled"`
	Prefix              string                          `json:"prefix,omitempty"`
	BaseURL             string                          `json:"base-url"`
	APIKeyEntries       []liteLLMAPIKeyWithAuthIndex    `json:"api-key-entries,omitempty"`
	Models              []config.LiteLLMModel           `json:"models,omitempty"`
	Headers             map[string]string               `json:"headers,omitempty"`
	DisableCooling      *bool                           `json:"disable-cooling,omitempty"`
	RequestRetry        *int                            `json:"request-retry,omitempty"`
	RequestScopedErrors []config.RequestScopedErrorRule `json:"request-scoped-errors,omitempty"`
	AuthIndex           string                          `json:"auth-index,omitempty"`
}

func (h *Handler) liteLLMWithAuthIndex() []liteLLMProviderWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}
	idGen := synthesizer.NewStableIDGenerator()
	out := make([]liteLLMProviderWithAuthIndex, len(h.cfg.LiteLLM))
	for i := range h.cfg.LiteLLM {
		entry := h.cfg.LiteLLM[i]
		name := strings.TrimSpace(entry.Name)
		response := liteLLMProviderWithAuthIndex{Name: entry.Name, Priority: entry.Priority, Disabled: entry.Disabled, Prefix: entry.Prefix, BaseURL: entry.BaseURL, Models: entry.Models, Headers: entry.Headers, DisableCooling: entry.DisableCooling, RequestRetry: entry.RequestRetry, RequestScopedErrors: entry.RequestScopedErrors}
		idKind := fmt.Sprintf("litellm:%s", strings.ToLower(name))
		if len(entry.APIKeyEntries) == 0 {
			id, _ := idGen.Next(idKind, entry.BaseURL, entry.Prefix, config.FormatSortedHeaders(entry.Headers))
			response.AuthIndex = liveIndexByID[id]
		} else {
			response.APIKeyEntries = make([]liteLLMAPIKeyWithAuthIndex, len(entry.APIKeyEntries))
			for j := range entry.APIKeyEntries {
				apiKeyEntry := entry.APIKeyEntries[j]
				id, _ := idGen.Next(idKind, apiKeyEntry.APIKey, entry.BaseURL, apiKeyEntry.ProxyURL, entry.Prefix, config.FormatSortedHeaders(entry.Headers))
				response.APIKeyEntries[j] = liteLLMAPIKeyWithAuthIndex{LiteLLMAPIKey: apiKeyEntry, AuthIndex: liveIndexByID[id]}
			}
		}
		out[i] = response
	}
	return out
}

func decodeLiteLLMProviders(c *gin.Context) ([]config.LiteLLMProvider, bool) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return nil, false
	}
	var items []config.LiteLLMProvider
	if err = json.Unmarshal(data, &items); err != nil {
		var envelope struct {
			Items []config.LiteLLMProvider `json:"items"`
		}
		if err = json.Unmarshal(data, &envelope); err != nil || envelope.Items == nil {
			c.JSON(400, gin.H{"error": "invalid body"})
			return nil, false
		}
		items = envelope.Items
	}
	for i := range items {
		for j := range items[i].APIKeyEntries {
			if rejectInvalidCredentialWeight(c, fmt.Sprintf("litellm[%d].api-key-entries[%d].weight", i, j), items[i].APIKeyEntries[j].Weight) {
				return nil, false
			}
		}
	}
	return items, true
}

func (h *Handler) GetLiteLLM(c *gin.Context) { c.JSON(200, gin.H{"litellm": h.liteLLMWithAuthIndex()}) }

func (h *Handler) PutLiteLLM(c *gin.Context) {
	items, ok := decodeLiteLLMProviders(c)
	if !ok {
		return
	}
	candidate := &config.Config{LiteLLM: items}
	candidate.SanitizeLiteLLM()
	if err := candidate.ValidateLiteLLMProviders(); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg.LiteLLM = candidate.LiteLLM
	h.persistLocked(c)
}

func (h *Handler) PatchLiteLLM(c *gin.Context) {
	var body struct {
		Name  *string                 `json:"name"`
		Index *int                    `json:"index"`
		Value *config.LiteLLMProvider `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	index := -1
	if body.Index != nil && *body.Index >= 0 && *body.Index < len(h.cfg.LiteLLM) {
		index = *body.Index
	}
	if index < 0 && body.Name != nil {
		for i := range h.cfg.LiteLLM {
			if strings.EqualFold(strings.TrimSpace(h.cfg.LiteLLM[i].Name), strings.TrimSpace(*body.Name)) {
				index = i
				break
			}
		}
	}
	if index < 0 {
		c.JSON(404, gin.H{"error": "item not found"})
		return
	}
	updated := h.cfg.LiteLLM[index]
	if body.Value.Name != "" {
		updated.Name = body.Value.Name
	}
	if body.Value.BaseURL != "" {
		updated.BaseURL = body.Value.BaseURL
	}
	if body.Value.Prefix != "" {
		updated.Prefix = body.Value.Prefix
	}
	if body.Value.Priority != 0 {
		updated.Priority = body.Value.Priority
	}
	updated.Disabled = body.Value.Disabled
	if body.Value.APIKeyEntries != nil {
		updated.APIKeyEntries = body.Value.APIKeyEntries
	}
	if body.Value.Models != nil {
		updated.Models = body.Value.Models
	}
	if body.Value.Headers != nil {
		updated.Headers = body.Value.Headers
	}
	if body.Value.DisableCooling != nil {
		updated.DisableCooling = body.Value.DisableCooling
	}
	if body.Value.RequestRetry != nil {
		updated.RequestRetry = body.Value.RequestRetry
	}
	if body.Value.RequestScopedErrors != nil {
		updated.RequestScopedErrors = body.Value.RequestScopedErrors
	}
	candidate := append([]config.LiteLLMProvider(nil), h.cfg.LiteLLM...)
	candidate[index] = updated
	check := &config.Config{LiteLLM: candidate}
	check.SanitizeLiteLLM()
	if err := check.ValidateLiteLLMProviders(); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	h.cfg.LiteLLM = check.LiteLLM
	h.persistLocked(c)
}

func (h *Handler) DeleteLiteLLM(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	indexText := strings.TrimSpace(c.Query("index"))
	index := -1
	if indexText != "" {
		if _, err := fmt.Sscanf(indexText, "%d", &index); err != nil {
			index = -1
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if name == "" && (index < 0 || index >= len(h.cfg.LiteLLM)) {
		c.JSON(400, gin.H{"error": "missing name or valid index"})
		return
	}
	out := make([]config.LiteLLMProvider, 0, len(h.cfg.LiteLLM))
	for i, item := range h.cfg.LiteLLM {
		if (name != "" && strings.EqualFold(strings.TrimSpace(item.Name), name)) || (name == "" && i == index) {
			continue
		}
		out = append(out, item)
	}
	h.cfg.LiteLLM = out
	h.cfg.SanitizeLiteLLM()
	h.persistLocked(c)
}

// end
