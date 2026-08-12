package claude

import (
	"testing"

	. "github.com/router-for-me/CLIProxyAPI/v7/internal/constant"
	itranslator "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/translator"
	"github.com/tidwall/gjson"
)

func TestClaudeCommandCodeRegistered(t *testing.T) {
	out := itranslator.Request(Claude, CommandCode, "deepseek/deepseek-v4-pro", []byte(`{"messages":[{"role":"user","content":"Hello"}]}`), true)
	if gjson.GetBytes(out, "memory").Type != gjson.String {
		t.Fatalf("registry did not use Claude mapper: %s", out)
	}
	if !gjson.GetBytes(out, "params.messages").IsArray() {
		t.Fatalf("params.messages: %s", out)
	}
}
