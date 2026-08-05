package responses

import (
	"context"

	ccchat "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/commandcode/openai/chat-completions"
	oairesp "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/openai/openai/responses"
	"github.com/tidwall/gjson"
)

// composedStreamParam holds independent *param state for each hop.
type composedStreamParam struct {
	viaChat  any
	toClient any
	doneSent bool
}

func ensureComposedStreamParam(param *any) *composedStreamParam {
	if param == nil {
		// Callers should pass non-nil; defensive local holder.
		return &composedStreamParam{}
	}
	if *param == nil {
		s := &composedStreamParam{}
		*param = s
		return s
	}
	if s, ok := (*param).(*composedStreamParam); ok && s != nil {
		return s
	}
	s := &composedStreamParam{}
	*param = s
	return s
}

// ConvertCommandCodeResponseToOpenAIResponses converts one CommandCode NDJSON line
// into zero or more OpenAI Responses SSE/event payloads via chat hop.
func ConvertCommandCodeResponseToOpenAIResponses(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	st := ensureComposedStreamParam(param)
	chatChunks := ccchat.ConvertCommandCodeResponseToOpenAI(ctx, modelName, originalRequestRawJSON, requestRawJSON, rawJSON, &st.viaChat)

	out := make([][]byte, 0)
	sawFinish := false
	if typ := gjson.GetBytes(rawJSON, "type").String(); typ == "finish" {
		sawFinish = true
	}
	for _, ch := range chatChunks {
		if fr := gjson.GetBytes(ch, "choices.0.finish_reason"); fr.Exists() && fr.Type != gjson.Null {
			sawFinish = true
		}
		out = append(out, oairesp.ConvertOpenAIChatCompletionsResponseToOpenAIResponses(ctx, modelName, originalRequestRawJSON, requestRawJSON, ch, &st.toClient)...)
	}
	// CommandCode NDJSON has no [DONE]; Responses completed is deferred until DONE.
	if sawFinish && !st.doneSent {
		st.doneSent = true
		out = append(out, oairesp.ConvertOpenAIChatCompletionsResponseToOpenAIResponses(ctx, modelName, originalRequestRawJSON, requestRawJSON, []byte("[DONE]"), &st.toClient)...)
	}
	return out
}

// ConvertCommandCodeResponseToOpenAIResponsesNonStream folds full NDJSON into one Responses JSON object.
func ConvertCommandCodeResponseToOpenAIResponsesNonStream(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	chat := ccchat.ConvertCommandCodeResponseToOpenAINonStream(ctx, modelName, originalRequestRawJSON, requestRawJSON, rawJSON, nil)
	return oairesp.ConvertOpenAIChatCompletionsResponseToOpenAIResponsesNonStream(ctx, modelName, originalRequestRawJSON, requestRawJSON, chat, nil)
}
