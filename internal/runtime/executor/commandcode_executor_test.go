package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestCommandCodeExecutor_HeadersAndPath(t *testing.T) {
	var gotAuth, gotSession, gotVer, gotEnv, gotCT string
	var path string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotSession = r.Header.Get("x-session-id")
		gotVer = r.Header.Get("x-command-code-version")
		gotEnv = r.Header.Get("x-cli-environment")
		gotCT = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"type":"text-delta","text":"ok"}` + "\n" + `{"type":"finish","finishReason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	exec := NewCommandCodeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "commandcode",
		Attributes: map[string]string{
			"api_key":  "user_abc",
			"base_url": srv.URL,
		},
	}
	payload := []byte(`{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`)
	result, err := exec.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "test-model",
		Payload: payload,
	}, cliproxyexecutor.Options{
		Stream:          true,
		SourceFormat:    sdktranslator.FormatOpenAI,
		OriginalRequest: payload,
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	var out strings.Builder
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error = %v", chunk.Err)
		}
		out.Write(chunk.Payload)
	}

	if path != "/alpha/generate" {
		t.Fatalf("path=%s, want /alpha/generate", path)
	}
	if gotAuth != "Bearer user_abc" {
		t.Fatalf("auth=%s, want Bearer user_abc", gotAuth)
	}
	if gotSession == "" {
		t.Fatal("missing x-session-id")
	}
	if gotVer != "0.25.7" {
		t.Fatalf("ver=%s, want 0.25.7", gotVer)
	}
	if gotEnv != "cli" {
		t.Fatalf("env=%s, want cli", gotEnv)
	}
	if !strings.Contains(gotCT, "application/json") {
		t.Fatalf("Content-Type=%s, want application/json", gotCT)
	}
	if !gjson.GetBytes(gotBody, "params.stream").Bool() {
		t.Fatalf("params.stream not true; body=%s", string(gotBody))
	}
	if out.Len() == 0 {
		t.Fatal("expected non-empty stream output")
	}
	if !strings.Contains(out.String(), `"object":"chat.completion.chunk"`) {
		t.Fatalf("stream output missing OpenAI chunk: %s", out.String())
	}
}

func TestCommandCodeExecutor_ExecuteNonStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"type":"text-delta","text":"hello"}` + "\n" + `{"type":"finish","finishReason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	exec := NewCommandCodeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "commandcode",
		Attributes: map[string]string{
			"api_key":  "key",
			"base_url": srv.URL,
		},
	}
	payload := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	resp, err := exec.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "m",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FormatOpenAI,
		OriginalRequest: payload,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := gjson.GetBytes(resp.Payload, "object").String(); got != "chat.completion" {
		t.Fatalf("object=%s, want chat.completion; payload=%s", got, string(resp.Payload))
	}
	if got := gjson.GetBytes(resp.Payload, "choices.0.message.content").String(); got != "hello" {
		t.Fatalf("content=%q, want hello", got)
	}
}

func TestCommandCodeExecutor_IdentifierAndCountTokens(t *testing.T) {
	exec := NewCommandCodeExecutor(&config.Config{})
	if got := exec.Identifier(); got != "commandcode" {
		t.Fatalf("Identifier()=%s", got)
	}
	_, err := exec.CountTokens(context.Background(), &cliproxyauth.Auth{}, cliproxyexecutor.Request{}, cliproxyexecutor.Options{})
	if err == nil {
		t.Fatal("CountTokens() expected error")
	}
	refreshed, err := exec.Refresh(context.Background(), &cliproxyauth.Auth{ID: "a"})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if refreshed == nil || refreshed.ID != "a" {
		t.Fatalf("Refresh() = %+v", refreshed)
	}
}

func TestCommandCodeExecutor_StatusErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	exec := NewCommandCodeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"api_key": "bad", "base_url": srv.URL},
	}
	payload := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	_, err := exec.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model: "m", Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI})
	assertStatusErr(t, err, http.StatusUnauthorized)
}

// TestCommandCodeExecutor_StreamEndsWithoutFinishEvent verifies that when the
// upstream NDJSON body ends without the terminal "finish" event (e.g. a
// max-tokens cutoff or an aborted response), the executor injects a synthetic
// finish so the client still receives a terminal finish_reason chunk instead of
// seeing the session as interrupted/truncated.
func TestCommandCodeExecutor_StreamEndsWithoutFinishEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"type":"text-delta","text":"partial answer"}` + "\n"))
		// NOTE: no {"type":"finish"} event; the body just ends.
	}))
	defer srv.Close()

	exec := NewCommandCodeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "commandcode",
		Attributes: map[string]string{
			"api_key":  "k",
			"base_url": srv.URL,
		},
	}
	payload := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	result, err := exec.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model: "m", Payload: payload,
	}, cliproxyexecutor.Options{
		Stream: true, SourceFormat: sdktranslator.FormatOpenAI, OriginalRequest: payload,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error = %v", err)
	}
	var chunks []string
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		chunks = append(chunks, string(chunk.Payload))
	}
	if len(chunks) < 2 {
		t.Fatalf("expected content + terminal chunk, got %d chunks: %v", len(chunks), chunks)
	}
	last := chunks[len(chunks)-1]
	if fr := gjson.GetBytes([]byte(last), "choices.0.finish_reason").String(); fr != "stop" {
		t.Fatalf("last chunk missing finish_reason=stop, got %q: %s", fr, last)
	}
}

func TestCommandCodeExecutor_StreamTrueAfterPayloadOverride(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"type":"text-delta","text":"x"}` + "\n" + `{"type":"finish","finishReason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Override: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{Name: "ovr-model"}},
				Params: map[string]any{"params.stream": false},
			}},
		},
	}
	exec := NewCommandCodeExecutor(cfg)
	auth := &cliproxyauth.Auth{
		Provider: "commandcode",
		Attributes: map[string]string{
			"api_key":  "k",
			"base_url": srv.URL,
		},
	}
	payload := []byte(`{"model":"ovr-model","messages":[{"role":"user","content":"hi"}]}`)
	_, err := exec.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "ovr-model",
		Payload: payload,
	}, cliproxyexecutor.Options{
		Stream:          true,
		SourceFormat:    sdktranslator.FormatOpenAI,
		OriginalRequest: payload,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error=%v", err)
	}
	if !gjson.GetBytes(gotBody, "params.stream").Bool() {
		t.Fatalf("params.stream must be true after ApplyPayloadConfigWithRequest; body=%s", string(gotBody))
	}
}
