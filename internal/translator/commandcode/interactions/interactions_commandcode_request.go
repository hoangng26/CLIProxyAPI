package interactions

import (
	ccchat "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/commandcode/openai/chat-completions"
	oiachat "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/openai/interactions/chat-completions"
)

// ConvertInteractionsRequestToCommandCode maps Interactions → chat → CommandCode envelope.
func ConvertInteractionsRequestToCommandCode(modelName string, rawJSON []byte, stream bool) []byte {
	chat := oiachat.ConvertInteractionsRequestToOpenAI(modelName, rawJSON, stream)
	return ccchat.ConvertOpenAIRequestToCommandCode(modelName, chat, stream)
}
