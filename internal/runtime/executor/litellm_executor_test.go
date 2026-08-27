package executor

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func TestLiteLLMTargetForFormat(t *testing.T) {
	cases := []struct {
		format sdktranslator.Format
		path   string
	}{
		{sdktranslator.FormatOpenAI, "/v1/chat/completions"},
		{sdktranslator.FormatOpenAIResponse, "/v1/responses"},
		{sdktranslator.FormatClaude, "/v1/messages"},
	}
	for _, tc := range cases {
		path, ok := liteLLMTargetForFormat(tc.format)
		if !ok || path != tc.path {
			t.Fatalf("format %q = %q, %v", tc.format, path, ok)
		}
	}
	if _, ok := liteLLMTargetForFormat(sdktranslator.FormatGemini); ok {
		t.Fatal("Gemini format accepted")
	}
}

func TestLiteLLMExecutorRequestToFormat(t *testing.T) {
	e := NewLiteLLMExecutor("litellm-prod", &config.Config{})
	for _, tc := range []struct {
		source, want string
	}{
		{"openai", "openai"}, {"openai-response", "openai-response"}, {"claude", "claude"},
	} {
		got := e.RequestToFormat(cliproxyexecutor.Request{}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString(tc.source)})
		if got.String() != tc.want {
			t.Fatalf("source %q => %q", tc.source, got)
		}
	}
}

func TestLiteLLMExecutorRejectsUnsupportedFormatBeforeRequest(t *testing.T) {
	e := NewLiteLLMExecutor("litellm-prod", &config.Config{})
	_, err := e.Execute(context.Background(), nil, cliproxyexecutor.Request{Model: "model", Payload: []byte(`{"model":"model"}`)}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatGemini})
	if err == nil {
		t.Fatal("expected unsupported format error")
	}
}

func TestLiteLLMExecutorRejectsMissingCredentials(t *testing.T) {
	e := NewLiteLLMExecutor("litellm-prod", &config.Config{})
	_, err := e.Execute(context.Background(), nil, cliproxyexecutor.Request{Model: "model", Payload: []byte(`{"model":"model"}`)}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI})
	if err == nil {
		t.Fatal("expected missing credential error")
	}
}

var _ = context.Background
var _ = config.Config{}
var _ = sdktranslator.FormatOpenAI
var _ = cliproxyexecutor.Request{}

// end
