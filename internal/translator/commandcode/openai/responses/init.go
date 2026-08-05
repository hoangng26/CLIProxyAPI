package responses

import (
	. "github.com/router-for-me/CLIProxyAPI/v7/internal/constant"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/translator/translator"
)

func init() {
	translator.Register(
		OpenaiResponse,
		CommandCode,
		ConvertOpenAIResponsesRequestToCommandCode,
		interfaces.TranslateResponse{
			Stream:    ConvertCommandCodeResponseToOpenAIResponses,
			NonStream: ConvertCommandCodeResponseToOpenAIResponsesNonStream,
		},
	)
}
