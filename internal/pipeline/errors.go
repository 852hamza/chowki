package pipeline

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/852hamza/chowki/internal/usage"
)

// apiError is an error that the gateway itself returns. Code is stable and
// machine-readable; Message says what to do.
type apiError struct {
	Status  int
	Code    string
	Message string
}

// Error codes of the gateway's own errors.
const (
	codeMissingKey         = "missing_api_key"
	codeInvalidKey         = "invalid_api_key"
	codeRevokedKey         = "revoked_api_key"
	codeInvalidRequest     = "invalid_request"
	codeTooLarge           = "request_too_large"
	codeBudgetExceeded     = "budget_exceeded"
	codeRateLimited        = "rate_limit_exceeded"
	codeSensitiveData      = "sensitive_data_blocked"
	codeModelNotAllowed    = "model_not_allowed"
	codeProviderKeyMissing = "provider_key_missing"
	codeUpstreamFailed     = "upstream_unavailable"
	codeUpstreamTimeout    = "upstream_timeout"
	codeInternal           = "internal_error"
	codeNotFound           = "not_found"
	codeUnsupportedOption  = "unsupported_option"
)

// writeError answers in the error format of the API family, so the
// client's SDK shows the error correctly. param names the request option at
// fault, if any, for the OpenAI format.
func writeError(w http.ResponseWriter, f usage.Family, requestID string, e apiError, param string) {
	var body any
	switch f {
	case usage.Gemini:
		// The Google API error model, https://google.aip.dev/193, with the
		// gateway's code as the reason of an ErrorInfo.
		body = map[string]any{"error": map[string]any{
			"code": e.Status, "message": e.Message, "status": grpcStatus(e.Status),
			"details": []any{map[string]any{"@type": "type.googleapis.com/google.rpc.ErrorInfo",
				"reason": strings.ToUpper(e.Code), "domain": "chowki"}},
		}}
	case usage.Anthropic:
		// https://platform.claude.com/docs/en/api/errors
		body = map[string]any{
			"type":       "error",
			"error":      map[string]any{"type": anthropicType(e.Status), "message": e.Message},
			"request_id": requestID,
		}
	default:
		// ErrorResponse in https://github.com/openai/openai-openapi
		var p any
		if param != "" {
			p = param
		}
		body = map[string]any{
			"error": map[string]any{"message": e.Message, "type": openAIType(e.Status), "param": p, "code": e.Code},
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.Status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // as the providers write their errors; see translate.marshal
	_ = enc.Encode(body)     // the client may be gone; nothing to do then
}

func openAIType(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return "authentication_error"
	case status == http.StatusForbidden:
		return "permission_error"
	case status == http.StatusTooManyRequests:
		return "rate_limit_error"
	case status >= 500:
		return "server_error"
	default:
		return "invalid_request_error"
	}
}

// grpcStatus maps a status to the google.rpc.Code that Google APIs send
// with it, following the HTTP mapping in google/rpc/code.proto.
func grpcStatus(status int) string {
	switch status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge:
		return "INVALID_ARGUMENT"
	case http.StatusUnauthorized:
		return "UNAUTHENTICATED"
	case http.StatusForbidden:
		return "PERMISSION_DENIED"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusTooManyRequests:
		return "RESOURCE_EXHAUSTED"
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return "UNAVAILABLE"
	case http.StatusGatewayTimeout:
		return "DEADLINE_EXCEEDED"
	case http.StatusInternalServerError:
		return "INTERNAL"
	}
	return "UNKNOWN"
}

// anthropicType maps a status to the error types that Anthropic documents.
func anthropicType(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusGatewayTimeout:
		return "timeout_error"
	}
	if status >= 500 {
		return "api_error"
	}
	return "invalid_request_error"
}
