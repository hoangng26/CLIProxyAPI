package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestLiteLLMManagementCRUD(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	router := gin.New()
	router.GET("/litellm", h.GetLiteLLM)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/litellm", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"litellm":`) {
		t.Fatalf("GET status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestLiteLLMManagementRejectsV1RootAndDuplicateNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	router := gin.New()
	router.PUT("/litellm", h.PutLiteLLM)

	for _, body := range []string{
		`[{"name":"prod","base-url":"http://localhost:4000/v1","api-key-entries":[{"api-key":"key"}]}]`,
		`[{"name":"prod","base-url":"http://one","api-key-entries":[{"api-key":"a"}]},{"name":"Prod","base-url":"http://two","api-key-entries":[{"api-key":"b"}]}]`,
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/litellm", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
	}
	if len(h.cfg.LiteLLM) != 0 {
		t.Fatalf("config mutated: %+v", h.cfg.LiteLLM)
	}
}
