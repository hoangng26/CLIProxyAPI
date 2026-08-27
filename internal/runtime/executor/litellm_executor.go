package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	log "github.com/sirupsen/logrus"
)

type LiteLLMExecutor struct {
	provider string
	cfg      *config.Config
}

func NewLiteLLMExecutor(provider string, cfg *config.Config) *LiteLLMExecutor {
	return &LiteLLMExecutor{provider: strings.ToLower(strings.TrimSpace(provider)), cfg: cfg}
}
func (e *LiteLLMExecutor) Identifier() string {
	if e == nil || e.provider == "" {
		return "litellm"
	}
	return e.provider
}
func (e *LiteLLMExecutor) RequestToFormat(_ cliproxyexecutor.Request, opts cliproxyexecutor.Options) sdktranslator.Format {
	switch opts.SourceFormat {
	case sdktranslator.FormatOpenAI, sdktranslator.FormatOpenAIResponse, sdktranslator.FormatClaude:
		return opts.SourceFormat
	default:
		return sdktranslator.Format("")
	}
}
func liteLLMTargetForFormat(format sdktranslator.Format) (string, bool) {
	switch format {
	case sdktranslator.FormatOpenAI:
		return "/v1/chat/completions", true
	case sdktranslator.FormatOpenAIResponse:
		return "/v1/responses", true
	case sdktranslator.FormatClaude:
		return "/v1/messages", true
	default:
		return "", false
	}
}

func (e *LiteLLMExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	if auth != nil && auth.Attributes != nil {
		if key := strings.TrimSpace(auth.Attributes["api_key"]); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	attrs := map[string]string(nil)
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(req, attrs)
	return nil
}
func (e *LiteLLMExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("litellm executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	request := req.WithContext(ctx)
	if err := e.PrepareRequest(request, auth); err != nil {
		return nil, err
	}
	return helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0).Do(request)
}

func (e *LiteLLMExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	format := e.RequestToFormat(req, opts)
	path, ok := liteLLMTargetForFormat(format)
	if !ok {
		return resp, fmt.Errorf("litellm executor: unsupported source format %q", format)
	}
	baseURL, apiKey := e.credentials(auth)
	if baseURL == "" || apiKey == "" {
		return resp, statusErr{code: http.StatusUnauthorized, msg: "missing LiteLLM credentials"}
	}
	model := strings.TrimSpace(thinking.ParseSuffix(req.Model).ModelName)
	payload := helps.SetStringIfDifferent(req.Payload, "model", e.resolveModel(auth, model))
	payload, err = helps.ApplyRequestThinking(payload, req, opts, format.String(), format.String(), e.Identifier())
	if err != nil {
		return resp, err
	}
	payload = helps.ApplyPayloadConfigWithRequest(e.cfg, model, format.String(), format.String(), "", payload, opts.OriginalRequest, helps.PayloadRequestedModel(opts, req.Model), helps.PayloadRequestPath(opts), opts.Headers)
	url := strings.TrimRight(baseURL, "/") + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return resp, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "cli-proxy-litellm")
	_ = apiKey
	if err = e.PrepareRequest(httpReq, auth); err != nil {
		return resp, err
	}
	reporter := helps.NewExecutorUsageReporter(ctx, e, model, auth)
	defer reporter.TrackFailure(ctx, &err)
	reporter.SetTranslatedReasoningEffort(payload, format.String())
	httpResp, err := reporter.TrackHTTPClient(helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)).Do(httpReq)
	if err != nil {
		return resp, err
	}
	defer func() {
		if closeErr := httpResp.Body.Close(); closeErr != nil {
			log.WithError(closeErr).Debug("litellm executor: close response body")
		}
	}()
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return resp, err
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return resp, statusErr{code: httpResp.StatusCode, msg: string(body)}
	}
	if format == sdktranslator.FormatClaude {
		reporter.Publish(ctx, helps.ParseClaudeUsage(body))
	} else {
		reporter.Publish(ctx, helps.ParseOpenAIUsage(body))
	}
	reporter.EnsurePublished(ctx)
	return cliproxyexecutor.Response{Payload: body, Headers: httpResp.Header.Clone()}, nil
}

func (e *LiteLLMExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	format := e.RequestToFormat(req, opts)
	path, ok := liteLLMTargetForFormat(format)
	if !ok {
		return nil, fmt.Errorf("litellm executor: unsupported source format %q", format)
	}
	base, key := e.credentials(auth)
	if base == "" || key == "" {
		return nil, statusErr{code: http.StatusUnauthorized, msg: "missing LiteLLM credentials"}
	}
	model := strings.TrimSpace(thinking.ParseSuffix(req.Model).ModelName)
	payload := helps.SetStringIfDifferent(req.Payload, "model", e.resolveModel(auth, model))
	url := strings.TrimRight(base, "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("User-Agent", "cli-proxy-litellm")
	if err = e.PrepareRequest(request, auth); err != nil {
		return nil, err
	}
	response, err := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0).Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		return nil, statusErr{code: response.StatusCode, msg: string(body)}
	}
	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		defer response.Body.Close()
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(nil, 52_428_800)
		for scanner.Scan() {
			chunk := bytes.Clone(append(scanner.Bytes(), '\n'))
			helps.AppendAPIResponseChunk(ctx, e.cfg, chunk)
			select {
			case out <- cliproxyexecutor.StreamChunk{Payload: chunk}:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case out <- cliproxyexecutor.StreamChunk{Err: err}:
			case <-ctx.Done():
			}
		}
	}()
	return &cliproxyexecutor.StreamResult{Headers: response.Header.Clone(), Chunks: out}, nil
}

func (e *LiteLLMExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	format := e.RequestToFormat(req, opts)
	if _, ok := liteLLMTargetForFormat(format); !ok {
		return cliproxyexecutor.Response{}, fmt.Errorf("litellm executor: unsupported source format %q", format)
	}
	enc, err := helps.TokenizerForModel(req.Model)
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}
	count, err := helps.CountOpenAIChatTokens(enc, req.Payload)
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}
	return cliproxyexecutor.Response{Payload: helps.BuildOpenAIUsageJSON(count)}, nil
}
func (e *LiteLLMExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if refreshed, handled, err := helps.RefreshAuthViaHome(ctx, e.cfg, auth); handled {
		return refreshed, err
	}
	return auth, nil
}
func (e *LiteLLMExecutor) credentials(auth *cliproxyauth.Auth) (string, string) {
	if auth == nil {
		return "", ""
	}
	return strings.TrimSpace(auth.Attributes["base_url"]), strings.TrimSpace(auth.Attributes["api_key"])
}
func (e *LiteLLMExecutor) resolveModel(auth *cliproxyauth.Auth, requested string) string {
	if e == nil || e.cfg == nil {
		return requested
	}
	provider := ""
	if auth != nil {
		provider = auth.Provider
	}
	for i := range e.cfg.LiteLLM {
		p := &e.cfg.LiteLLM[i]
		if p.Disabled || !strings.EqualFold("litellm-"+strings.ToLower(strings.TrimSpace(p.Name)), provider) {
			continue
		}
		for _, m := range p.Models {
			alias := strings.TrimSpace(m.Alias)
			if alias == "" {
				alias = m.Name
			}
			if strings.EqualFold(alias, requested) {
				return strings.TrimSpace(m.Name)
			}
		}
	}
	return requested
}

var _ cliproxyauth.ProviderExecutor = (*LiteLLMExecutor)(nil)
