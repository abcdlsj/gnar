package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func NewRequest(ctx context.Context, baseURL, method, endpoint string, body io.Reader) (*http.Request, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	relative, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	return http.NewRequestWithContext(ctx, method, base.ResolveReference(relative).String(), body)
}

func NewJSONRequest(ctx context.Context, baseURL, method, endpoint string, payload any) (*http.Request, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := NewRequest(ctx, baseURL, method, endpoint, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]string{"error": message})
}

func DecodeError(resp *http.Response) error {
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil && payload.Error != "" {
		return fmt.Errorf(payload.Error)
	}
	return fmt.Errorf("unexpected status: %s", resp.Status)
}

func RequestToken(r *http.Request) string {
	token := r.URL.Query().Get("token")
	if token != "" {
		return token
	}
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	}
	return ""
}

func HeaderToMap(header http.Header) map[string][]string {
	result := make(map[string][]string, len(header))
	for key, values := range header {
		if ShouldSkipHeader(key) {
			continue
		}
		copied := make([]string, len(values))
		copy(copied, values)
		result[key] = copied
	}
	return result
}

func CloneHeaderMap(values map[string][]string) map[string][]string {
	cloned := make(map[string][]string, len(values))
	for key, items := range values {
		if ShouldSkipHeader(key) {
			continue
		}
		copied := make([]string, len(items))
		copy(copied, items)
		cloned[key] = copied
	}
	return cloned
}

func ShouldSkipHeader(key string) bool {
	switch strings.ToLower(key) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailers", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}
