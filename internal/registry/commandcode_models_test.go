package registry

import (
	"testing"
)

func TestParseCommandCodeRemoteModels(t *testing.T) {
	raw := []byte(`{
		"object":"list",
		"data":[
			{"id":"deepseek/deepseek-v4-pro","object":"model","created":1,"owned_by":"command-code","name":"DeepSeek V4 Pro","context_length":1000000},
			{"id":"claude-opus-5","object":"model","created":2,"owned_by":"command-code","name":"Claude Opus 5","context_length":1000000},
			{"id":"deepseek/deepseek-v4-pro","object":"model","created":3,"owned_by":"command-code","name":"dup","context_length":1}
		]
	}`)
	models, err := parseCommandCodeRemoteModels(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len = %d, want 2 (duplicate id dropped)", len(models))
	}
	if models[0].ID != "deepseek/deepseek-v4-pro" {
		t.Fatalf("id0 = %q", models[0].ID)
	}
	if models[0].DisplayName != "DeepSeek V4 Pro" {
		t.Fatalf("display0 = %q", models[0].DisplayName)
	}
	if models[0].OwnedBy != "commandcode" || models[0].Type != "commandcode" {
		t.Fatalf("owned/type = %q/%q", models[0].OwnedBy, models[0].Type)
	}
	if models[0].ContextLength != 1000000 || models[0].InputTokenLimit != 1000000 {
		t.Fatalf("context = %d input = %d", models[0].ContextLength, models[0].InputTokenLimit)
	}
	if models[1].ID != "claude-opus-5" {
		t.Fatalf("id1 = %q", models[1].ID)
	}
}

func TestParseCommandCodeRemoteModelsRejectsEmpty(t *testing.T) {
	if _, err := parseCommandCodeRemoteModels([]byte(`{"object":"list","data":[]}`)); err == nil {
		t.Fatal("expected empty data error")
	}
}

func TestLoadCommandCodeModelsUpdatesGetCommandCodeModels(t *testing.T) {
	t.Cleanup(func() {
		_ = loadCommandCodeModels(commandCodeBuiltinModels(), "test-cleanup")
	})

	remote := []*ModelInfo{{
		ID:          "claude-opus-5",
		Object:      "model",
		DisplayName: "Claude Opus 5",
		OwnedBy:     "commandcode",
		Type:        "commandcode",
		Name:        "claude-opus-5",
	}}
	if err := loadCommandCodeModels(remote, "test"); err != nil {
		t.Fatalf("load: %v", err)
	}
	got := GetCommandCodeModels()
	if len(got) != 1 || got[0].ID != "claude-opus-5" {
		t.Fatalf("got %#v", got)
	}
}

func TestGetCommandCodeModelsIncludesBuiltinFallback(t *testing.T) {
	t.Cleanup(func() {
		_ = loadCommandCodeModels(commandCodeBuiltinModels(), "test-cleanup")
	})
	_ = loadCommandCodeModels(commandCodeBuiltinModels(), "test-reset")

	models := GetCommandCodeModels()
	found := false
	for _, m := range models {
		if m != nil && m.ID == "deepseek/deepseek-v4-pro" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing deepseek/deepseek-v4-pro in builtin fallback")
	}
}
