package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	commandCodeDefaultBaseURL = "https://api.commandcode.ai"
	commandCodeGeneratePath   = "/alpha/generate"
	commandCodeVersionHeader  = "0.25.7"
)

// CommandCodeExecutor POSTs to CommandCode /alpha/generate and translates NDJSON streams.
type CommandCodeExecutor struct {
	cfg *config.Config
}

// NewCommandCodeExecutor creates a CommandCode executor bound to cfg.
func NewCommandCodeExecutor(cfg *config.Config) *CommandCodeExecutor {
	return &CommandCodeExecutor{cfg: cfg}
}

// Identifier returns the executor identifier.
func (e *CommandCodeExecutor) Identifier() string { return "commandcode" }

// PrepareRequest injects CommandCode credentials and fixed headers.
func (e *CommandCodeExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	if key := commandCodeAPIKey(auth); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	applyCommandCodeFixedHeaders(req)
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(req, attrs)
	return nil
}

// HttpRequest injects credentials and executes the HTTP request.
func (e *CommandCodeExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("commandcode executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if err := e.PrepareRequest(httpReq, auth); err != nil {
		return nil, err
	}
	httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}

// Execute performs a non-streaming request. Upstream is always streamed (NDJSON);
// the full body is folded via TranslateNonStream.
func (e *CommandCodeExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	reporter := helps.NewExecutorUsageReporter(ctx, e, baseModel, auth)
	defer reporter.TrackFailure(ctx, &err)

	from := opts.SourceFormat
	responseFormat := cliproxyexecutor.ResponseFormatOrSource(opts)
	to := sdktranslator.FromString("commandcode")

	body, errPrep := e.prepareBody(ctx, req, opts, from, to, baseModel)
	if errPrep != nil {
		return resp, errPrep
	}
	reporter.SetTranslatedReasoningEffort(body, e.Identifier())

	httpResp, translated, errDo := e.doUpstream(ctx, auth, body, reporter)
	if errDo != nil {
		return resp, errDo
	}
	defer func() {
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("commandcode executor: close response body error: %v", errClose)
		}
	}()

	data, errRead := io.ReadAll(httpResp.Body)
	if errRead != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, errRead)
		return resp, errRead
	}
	helps.AppendAPIResponseChunk(ctx, e.cfg, data)
	reporter.Publish(ctx, parseCommandCodeUsage(data))
	reporter.EnsurePublished(ctx)

	var param any
	out := sdktranslator.TranslateNonStream(ctx, to, responseFormat, req.Model, opts.OriginalRequest, translated, data, &param)
	resp = cliproxyexecutor.Response{Payload: out, Headers: httpResp.Header.Clone()}
	return resp, nil
}

// ExecuteStream streams NDJSON lines through the commandcode→responseFormat translator.
func (e *CommandCodeExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	reporter := helps.NewExecutorUsageReporter(ctx, e, baseModel, auth)
	defer reporter.TrackFailure(ctx, &err)

	from := opts.SourceFormat
	responseFormat := cliproxyexecutor.ResponseFormatOrSource(opts)
	to := sdktranslator.FromString("commandcode")

	body, errPrep := e.prepareBody(ctx, req, opts, from, to, baseModel)
	if errPrep != nil {
		return nil, errPrep
	}
	reporter.SetTranslatedReasoningEffort(body, e.Identifier())

	httpResp, translated, errDo := e.doUpstream(ctx, auth, body, reporter)
	if errDo != nil {
		return nil, errDo
	}

	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		defer func() {
			if errClose := httpResp.Body.Close(); errClose != nil {
				log.Errorf("commandcode executor: close response body error: %v", errClose)
			}
			reporter.EnsurePublished(ctx)
		}()
		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, 1_048_576)
		var param any
		// Terminal events ("finish" and "error") cause the responseFormat converter
		// to emit its own final chunk. If none is seen before the body ends, we
		// inject a synthetic finish so the client still gets a terminal chunk.
		sawTerminalEvent := false
		detectTerminalEvent := func(line []byte) bool {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				return false
			}
			if bytes.HasPrefix(line, []byte("data:")) {
				line = bytes.TrimSpace(line[5:])
			}
			switch gjson.GetBytes(line, "type").String() {
			case "finish", "error":
				return true
			default:
				return false
			}
		}
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			helps.AppendAPIResponseChunk(ctx, e.cfg, line)
			if detectTerminalEvent(line) {
				sawTerminalEvent = true
			}
			if u := parseCommandCodeUsageLine(line); u.TotalTokens > 0 || u.InputTokens > 0 || u.OutputTokens > 0 {
				reporter.Publish(ctx, u)
			}
			chunks := sdktranslator.TranslateStream(ctx, to, responseFormat, req.Model, opts.OriginalRequest, translated, bytes.Clone(line), &param)
			for i := range chunks {
				select {
				case out <- cliproxyexecutor.StreamChunk{Payload: chunks[i]}:
				case <-ctx.Done():
					return
				}
			}
		}
		if errScan := scanner.Err(); errScan != nil {
			helps.RecordAPIResponseError(ctx, e.cfg, errScan)
			reporter.PublishFailure(ctx, errScan)
			select {
			case out <- cliproxyexecutor.StreamChunk{Err: errScan}:
			case <-ctx.Done():
			}
			return
		}
		if !sawTerminalEvent {
			// Upstream ended without a terminal event (e.g. a mid-response abort or
			// max-token cutoff). Emit a synthetic finish so the client still sees a
			// completed stream instead of a truncated one. The responseFormat
			// converter finalizes its own terminal state from this synthetic finish.
			helps.RecordAPIResponseError(ctx, e.cfg, newCommandCodeIncompleteStreamError())
			chunks := sdktranslator.TranslateStream(ctx, to, responseFormat, req.Model, opts.OriginalRequest, translated, []byte(`{"type":"finish","finishReason":"stop"}`), &param)
			for i := range chunks {
				select {
				case out <- cliproxyexecutor.StreamChunk{Payload: chunks[i]}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return &cliproxyexecutor.StreamResult{Headers: httpResp.Header.Clone(), Chunks: out}, nil
}

// CountTokens is not implemented for CommandCode.
func (e *CommandCodeExecutor) CountTokens(context.Context, *cliproxyauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, statusErr{code: http.StatusNotImplemented, msg: "commandcode executor: CountTokens not implemented"}
}

// Refresh is a no-op passthrough for API-key auth.
func (e *CommandCodeExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	log.Debugf("commandcode executor: refresh called")
	if refreshed, handled, err := helps.RefreshAuthViaHome(ctx, e.cfg, auth); handled {
		return refreshed, err
	}
	return auth, nil
}

func (e *CommandCodeExecutor) prepareBody(ctx context.Context, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, from, to sdktranslator.Format, baseModel string) ([]byte, error) {
	originalPayloadSource := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayloadSource = opts.OriginalRequest
	}
	// Upstream always streams NDJSON.
	originalTranslated := helps.TranslateRequestWithCodexMultiAgentV2(ctx, opts.Headers, e.cfg, from, to, baseModel, bytes.Clone(originalPayloadSource), true)
	body := helps.TranslateRequestWithCodexMultiAgentV2(ctx, opts.Headers, e.cfg, from, to, baseModel, bytes.Clone(req.Payload), true)

	body, err := helps.ApplyRequestThinking(body, req, opts, from.String(), to.String(), e.Identifier())
	if err != nil {
		return nil, err
	}

	requestedModel := helps.PayloadRequestedModel(opts, req.Model)
	requestPath := helps.PayloadRequestPath(opts)
	body = helps.ApplyPayloadConfigWithRequest(e.cfg, baseModel, to.String(), from.String(), "", body, originalTranslated, requestedModel, requestPath, opts.Headers)

	body, err = sjson.SetBytes(body, "params.stream", true)
	if err != nil {
		return nil, fmt.Errorf("commandcode executor: failed to force params.stream: %w", err)
	}
	return body, nil
}

func (e *CommandCodeExecutor) doUpstream(ctx context.Context, auth *cliproxyauth.Auth, body []byte, reporter *helps.UsageReporter) (*http.Response, []byte, error) {
	baseURL, apiKey := commandCodeCreds(auth)
	if apiKey == "" {
		return nil, nil, statusErr{code: http.StatusUnauthorized, msg: "missing commandcode api_key"}
	}
	url := strings.TrimSuffix(baseURL, "/") + commandCodeGeneratePath

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	applyCommandCodeFixedHeaders(httpReq)
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(httpReq, attrs)

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	helps.RecordAPIRequest(ctx, e.cfg, helps.UpstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      body,
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpClient = reporter.TrackHTTPClient(httpClient)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, err)
		return nil, nil, err
	}
	helps.RecordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		helps.AppendAPIResponseChunk(ctx, e.cfg, b)
		helps.LogWithRequestID(ctx).Debugf("request error, error status: %d, error message: %s", httpResp.StatusCode, helps.SummarizeErrorBody(httpResp.Header.Get("Content-Type"), b))
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("commandcode executor: close response body error: %v", errClose)
		}
		return nil, nil, statusErr{code: httpResp.StatusCode, msg: string(b)}
	}
	return httpResp, body, nil
}

func applyCommandCodeFixedHeaders(r *http.Request) {
	if r == nil {
		return
	}
	r.Header.Set("x-session-id", uuid.NewString())
	r.Header.Set("x-command-code-version", commandCodeVersionHeader)
	r.Header.Set("x-cli-environment", "cli")
}

func commandCodeAPIKey(auth *cliproxyauth.Auth) string {
	if auth == nil || auth.Attributes == nil {
		return ""
	}
	if v := strings.TrimSpace(auth.Attributes["api_key"]); v != "" {
		return v
	}
	return strings.TrimSpace(auth.Attributes["access_token"])
}

func commandCodeCreds(auth *cliproxyauth.Auth) (baseURL, apiKey string) {
	apiKey = commandCodeAPIKey(auth)
	baseURL = commandCodeDefaultBaseURL
	if auth != nil && auth.Attributes != nil {
		if v := strings.TrimSpace(auth.Attributes["base_url"]); v != "" {
			baseURL = v
		}
	}
	return baseURL, apiKey
}

func parseCommandCodeUsage(data []byte) usage.Detail {
	var last usage.Detail
	for _, line := range bytes.Split(data, []byte("\n")) {
		if u := parseCommandCodeUsageLine(line); u.TotalTokens > 0 || u.InputTokens > 0 || u.OutputTokens > 0 {
			last = u
		}
	}
	if last.TotalTokens == 0 && (last.InputTokens > 0 || last.OutputTokens > 0) {
		last.TotalTokens = last.InputTokens + last.OutputTokens
	}
	return last
}

func parseCommandCodeUsageLine(line []byte) usage.Detail {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return usage.Detail{}
	}
	root := gjson.ParseBytes(line)
	node := root.Get("totalUsage")
	if !node.Exists() {
		node = root.Get("usage")
	}
	if !node.Exists() {
		return usage.Detail{}
	}
	u := usage.Detail{
		InputTokens:  firstCommandCodeInt(node, "promptTokens", "prompt_tokens", "inputTokens", "input_tokens"),
		OutputTokens: firstCommandCodeInt(node, "completionTokens", "completion_tokens", "outputTokens", "output_tokens"),
	}
	if v := node.Get("totalTokens"); v.Exists() {
		u.TotalTokens = v.Int()
	} else if v := node.Get("total_tokens"); v.Exists() {
		u.TotalTokens = v.Int()
	} else {
		u.TotalTokens = u.InputTokens + u.OutputTokens
	}
	return u
}

func firstCommandCodeInt(node gjson.Result, keys ...string) int64 {
	for _, k := range keys {
		if v := node.Get(k); v.Exists() {
			return v.Int()
		}
	}
	return 0
}

const commandCodeIncompleteStreamMessage = "stream error: stream disconnected before completion: CommandCode stream closed before the finish event"

// commandCodeIncompleteStreamError marks a CommandCode NDJSON stream that ended
// before the terminal "finish" event was received.
type commandCodeIncompleteStreamError struct {
	statusErr
}

func newCommandCodeIncompleteStreamError() commandCodeIncompleteStreamError {
	return commandCodeIncompleteStreamError{statusErr: statusErr{
		code: http.StatusRequestTimeout,
		msg:  commandCodeIncompleteStreamMessage,
	}}
}
