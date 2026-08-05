package responses

import (
	ccchat "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/commandcode/openai/chat-completions"
	oairesp "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/openai/openai/responses"
)

// ConvertOpenAIResponsesRequestToCommandCode maps Responses → chat → CommandCode envelope.
func ConvertOpenAIResponsesRequestToCommandCode(modelName string, rawJSON []byte, stream bool) []byte {
	chat := oairesp.ConvertOpenAIResponsesRequestToOpenAIChatCompletions(modelName, rawJSON, stream)
	return ccchat.ConvertOpenAIRequestToCommandCode(modelName, chat, stream)
}
