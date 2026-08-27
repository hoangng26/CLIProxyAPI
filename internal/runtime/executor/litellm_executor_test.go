package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
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

func TestLiteLLMExecutorForwardsNativeRequests(t *testing.T) {
	type received struct {
		path   string
		body   []byte
		auth   string
		custom string
	}
	receivedCh := make(chan received, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedCh <- received{path: r.URL.Path, body: body, auth: r.Header.Get("Authorization"), custom: r.Header.Get("X-Test")}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
	}))
	defer server.Close()

	cfg := &config.Config{}
	e := NewLiteLLMExecutor("litellm-prod", cfg)
	auth := &cliproxyauth.Auth{Provider: "litellm-prod", Attributes: map[string]string{
		"base_url": server.URL, "api_key": "secret", "header:X-Test": "yes",
	}}
	cases := []struct {
		source sdktranslator.Format
		path   string
		body   string
	}{
		{sdktranslator.FormatOpenAI, "/v1/chat/completions", `{"model":"upstream","messages":[{"role":"user","content":"hi"}],"extra":{"keep":true}}`},
		{sdktranslator.FormatOpenAIResponse, "/v1/responses", `{"model":"upstream","input":"hi","extra":{"keep":true}}`},
		{sdktranslator.FormatClaude, "/v1/messages", `{"model":"upstream","max_tokens":8,"messages":[{"role":"user","content":"hi"}],"extra":{"keep":true}}`},
	}
	for _, tc := range cases {
		_, err := e.Execute(context.Background(), auth, cliproxyexecutor.Request{Model: "upstream", Payload: []byte(tc.body)}, cliproxyexecutor.Options{SourceFormat: tc.source, ResponseFormat: tc.source, OriginalRequest: []byte(tc.body)})
		if err != nil {
			t.Fatalf("source %q: %v", tc.source, err)
		}
		got := <-receivedCh
		if got.path != tc.path || got.auth != "Bearer secret" || got.custom != "yes" {
			t.Fatalf("received = %+v", got)
		}
		if gjson.GetBytes(got.body, "extra.keep").Bool() != true {
			t.Fatalf("native field lost: %s", got.body)
		}
	}
}

func TestLiteLLMExecutorStreamPassesBytesUnchanged(t *testing.T) {
	const stream = "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(stream))
	}))
	defer server.Close()
	e := NewLiteLLMExecutor("litellm-prod", &config.Config{})
	auth := &cliproxyauth.Auth{Provider: "litellm-prod", Attributes: map[string]string{"base_url": server.URL, "api_key": "secret"}}
	result, err := e.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{Model: "model", Payload: []byte(`{"model":"model","stream":true}`)}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		got.Write(chunk.Payload)
	}
	if got.String() != stream {
		t.Fatalf("stream = %q, want %q", got.String(), stream)
	}
}

func TestLiteLLMExecutorPropagatesErrorStatusAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer server.Close()
	e := NewLiteLLMExecutor("litellm-prod", &config.Config{})
	auth := &cliproxyauth.Auth{Provider: "litellm-prod", Attributes: map[string]string{"base_url": server.URL, "api_key": "secret"}}
	_, err := e.Execute(context.Background(), auth, cliproxyexecutor.Request{Model: "model", Payload: []byte(`{"model":"model"}`)}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("rate limited")) {
		t.Fatalf("error = %v", err)
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

var _ = cliproxyexecutor.Response{}
