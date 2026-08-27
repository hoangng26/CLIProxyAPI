package cliproxy

import (
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/modelconfig"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func (s *Service) resolveConfigLiteLLM(authEntry *auth.Auth) *config.LiteLLMProvider {
	if s == nil || s.cfg == nil || authEntry == nil {
		return nil
	}
	if authEntry.Attributes != nil {
		if rawIndex := strings.TrimSpace(authEntry.Attributes[auth.AttributeConfigIndex]); rawIndex != "" {
			if index, err := strconv.Atoi(rawIndex); err == nil && index >= 0 && index < len(s.cfg.LiteLLM) && !s.cfg.LiteLLM[index].Disabled {
				return &s.cfg.LiteLLM[index]
			}
		}
		if name := strings.TrimSpace(authEntry.Attributes["config_name"]); name != "" {
			for index := range s.cfg.LiteLLM {
				if !s.cfg.LiteLLM[index].Disabled && strings.EqualFold(strings.TrimSpace(s.cfg.LiteLLM[index].Name), name) {
					return &s.cfg.LiteLLM[index]
				}
			}
		}
	}
	provider := strings.ToLower(strings.TrimSpace(authEntry.Provider))
	for index := range s.cfg.LiteLLM {
		name := strings.ToLower(strings.TrimSpace(s.cfg.LiteLLM[index].Name))
		if !s.cfg.LiteLLM[index].Disabled && provider == "litellm-"+name {
			return &s.cfg.LiteLLM[index]
		}
	}
	return nil
}

func buildLiteLLMConfigModels(provider *config.LiteLLMProvider) []*ModelInfo {
	if provider == nil || len(provider.Models) == 0 {
		return nil
	}
	models := make([]*ModelInfo, 0, len(provider.Models))
	seen := make(map[string]struct{}, len(provider.Models))
	for _, model := range provider.Models {
		name := strings.TrimSpace(model.Name)
		alias := strings.TrimSpace(model.Alias)
		if alias == "" {
			alias = name
		}
		if name == "" || alias == "" {
			continue
		}
		key := strings.ToLower(alias)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		info := &ModelInfo{
			ID:                        alias,
			Object:                    "model",
			Created:                   time.Now().Unix(),
			OwnedBy:                   provider.Name,
			Type:                      "litellm",
			DisplayName:               strings.TrimSpace(model.DisplayName),
			UserDefined:               true,
			ContextLength:             model.MaxContextLength,
			MaxContextLength:          model.MaxContextLength,
			IsCompat:                  model.IsCompat,
			SupportedInputModalities:  normalizeCompatConfigModalities(model.InputModalities),
			SupportedOutputModalities: normalizeCompatConfigModalities(model.OutputModalities),
		}
		if info.DisplayName == "" {
			info.DisplayName = alias
		}
		thinking := model.Thinking
		if thinking == nil {
			thinking = &registry.ThinkingSupport{Levels: []string{"low", "medium", "high"}}
		}
		info.Thinking = modelconfig.NormalizeThinkingSupport(thinking)
		models = append(models, info)
	}
	return models
}

func resolveUpstreamModelForLiteLLM(cfg *config.Config, authEntry *auth.Auth, requested string) string {
	if cfg == nil {
		return ""
	}
	var provider *config.LiteLLMProvider
	if authEntry != nil && authEntry.Attributes != nil {
		if rawIndex := strings.TrimSpace(authEntry.Attributes[auth.AttributeConfigIndex]); rawIndex != "" {
			if index, err := strconv.Atoi(rawIndex); err == nil && index >= 0 && index < len(cfg.LiteLLM) && !cfg.LiteLLM[index].Disabled {
				provider = &cfg.LiteLLM[index]
			}
		}
		if provider == nil {
			if name := strings.TrimSpace(authEntry.Attributes["config_name"]); name != "" {
				for index := range cfg.LiteLLM {
					if !cfg.LiteLLM[index].Disabled && strings.EqualFold(cfg.LiteLLM[index].Name, name) {
						provider = &cfg.LiteLLM[index]
						break
					}
				}
			}
		}
	}
	if provider == nil && authEntry != nil {
		providerKey := strings.ToLower(strings.TrimSpace(authEntry.Provider))
		for index := range cfg.LiteLLM {
			if !cfg.LiteLLM[index].Disabled && providerKey == "litellm-"+strings.ToLower(strings.TrimSpace(cfg.LiteLLM[index].Name)) {
				provider = &cfg.LiteLLM[index]
				break
			}
		}
	}
	if provider == nil {
		return ""
	}
	requested = strings.TrimSpace(requested)
	for _, model := range provider.Models {
		alias := strings.TrimSpace(model.Alias)
		if alias == "" {
			alias = strings.TrimSpace(model.Name)
		}
		if strings.EqualFold(alias, requested) {
			return strings.TrimSpace(model.Name)
		}
	}
	return requested
}

var _ = modelconfig.NormalizeThinkingSupport
var _ = time.Now
var _ = registry.ThinkingSupport{}
