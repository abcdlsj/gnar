package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	serverURL string
	token     string
	http      *http.Client
}

func NewClient(serverURL, token string) *Client {
	return &Client{
		serverURL: serverURL,
		token:     token,
		http:      &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *Client) ListTunnels(ctx context.Context, tenant string) (ListTunnelsResponse, error) {
	endpoint := "/api/v1/tunnels"
	if tenant != "" {
		endpoint += "?tenant=" + url.QueryEscape(tenant)
	}
	req, err := c.newRequest(ctx, http.MethodGet, endpoint)
	if err != nil {
		return ListTunnelsResponse{}, err
	}
	var response ListTunnelsResponse
	err = c.do(req, &response)
	return response, err
}

func (c *Client) TunnelDetail(ctx context.Context, tenant, name string) (TunnelDetailResponse, error) {
	endpoint := "/api/v1/tunnels/" + url.PathEscape(name)
	if tenant != "" {
		endpoint += "?tenant=" + url.QueryEscape(tenant)
	}
	req, err := c.newRequest(ctx, http.MethodGet, endpoint)
	if err != nil {
		return TunnelDetailResponse{}, err
	}
	var response TunnelDetailResponse
	err = c.do(req, &response)
	return response, err
}

func (c *Client) TunnelLogs(ctx context.Context, tenant, name string, limit int) (TunnelDetailResponse, error) {
	query := "limit=" + strconv.Itoa(limit)
	if tenant != "" {
		query += "&tenant=" + url.QueryEscape(tenant)
	}
	endpoint := "/api/v1/tunnels/" + url.PathEscape(name) + "/logs?" + query
	req, err := c.newRequest(ctx, http.MethodGet, endpoint)
	if err != nil {
		return TunnelDetailResponse{}, err
	}
	var raw struct {
		Tunnel   TunnelSummary     `json:"tunnel"`
		Requests []RequestLogEntry `json:"requests"`
	}
	if err := c.do(req, &raw); err != nil {
		return TunnelDetailResponse{}, err
	}
	return TunnelDetailResponse{
		Tunnel:         raw.Tunnel,
		RecentRequests: raw.Requests,
	}, nil
}

func (c *Client) Health(ctx context.Context) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/healthz")
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server health returned %s", resp.Status)
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, method, endpoint string) (*http.Request, error) {
	base, err := url.Parse(c.serverURL)
	if err != nil {
		return nil, err
	}
	relative, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, base.ResolveReference(relative).String(), nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var payload ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil && payload.Error != "" {
			return fmt.Errorf(payload.Error)
		}
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func TokenFromRequest(r *http.Request) string {
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
