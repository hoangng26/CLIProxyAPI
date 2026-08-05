package interactions

import (
	"context"

	ccchat "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/commandcode/openai/chat-completions"
	oiachat "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/openai/interactions/chat-completions"
	"github.com/tidwall/gjson"
)

type composedStreamParam struct {
	viaChat  any
	toClient any
	doneSent bool
}

func ensureComposedStreamParam(param *any) *composedStreamParam {
	if param == nil {
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

// ConvertCommandCodeResponseToInteractions converts one CommandCode NDJSON line to Interactions events.
func ConvertCommandCodeResponseToInteractions(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
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
		out = append(out, oiachat.ConvertOpenAIResponseToInteractions(ctx, modelName, originalRequestRawJSON, requestRawJSON, ch, &st.toClient)...)
	}
	if sawFinish && !st.doneSent {
		st.doneSent = true
		out = append(out, oiachat.ConvertOpenAIResponseToInteractions(ctx, modelName, originalRequestRawJSON, requestRawJSON, []byte("[DONE]"), &st.toClient)...)
	}
	return out
}

// ConvertCommandCodeResponseToInteractionsNonStream folds NDJSON to one Interactions JSON object.
func ConvertCommandCodeResponseToInteractionsNonStream(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	chat := ccchat.ConvertCommandCodeResponseToOpenAINonStream(ctx, modelName, originalRequestRawJSON, requestRawJSON, rawJSON, nil)
	return oiachat.ConvertOpenAIResponseToInteractionsNonStream(ctx, modelName, originalRequestRawJSON, requestRawJSON, chat, nil)
}
