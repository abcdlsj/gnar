package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/abcdlsj/gnar/internal/norm"
)

type DaemonClient struct {
	baseURL string
	http    *http.Client
}

func NewDaemonClient(baseURL string) *DaemonClient {
	return &DaemonClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *DaemonClient) Start(ctx context.Context, req StartTunnelRequest) (ManagedTunnel, error) {
	httpReq, err := c.newJSONRequest(ctx, http.MethodPost, "/api/v1/tunnels", req)
	if err != nil {
		return ManagedTunnel{}, err
	}
	var resp StartTunnelResponse
	if err := c.do(httpReq, &resp); err != nil {
		return ManagedTunnel{}, err
	}
	return resp.Tunnel, nil
}

func (c *DaemonClient) List(ctx context.Context) ([]ManagedTunnel, error) {
	httpReq, err := c.newRequest(ctx, http.MethodGet, "/api/v1/tunnels", nil)
	if err != nil {
		return nil, err
	}
	var resp ListManagedResponse
	if err := c.do(httpReq, &resp); err != nil {
		return nil, err
	}
	return resp.Tunnels, nil
}

func (c *DaemonClient) Stop(ctx context.Context, tenant, name string) error {
	path := "/api/v1/tunnels/" + url.PathEscape(name) + "?tenant=" + url.QueryEscape(norm.Tenant(tenant))
	httpReq, err := c.newRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	return c.do(httpReq, &map[string]string{})
}

func (c *DaemonClient) Health(ctx context.Context) error {
	httpReq, err := c.newRequest(ctx, http.MethodGet, "/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon returned %s", resp.Status)
	}
	return nil
}

func (c *DaemonClient) newJSONRequest(ctx context.Context, method, path string, payload any) (*http.Request, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, method, path, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (c *DaemonClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}
	relative, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	return http.NewRequestWithContext(ctx, method, base.ResolveReference(relative).String(), body)
}

func (c *DaemonClient) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var payload map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil && payload["error"] != "" {
			return fmt.Errorf(payload["error"])
		}
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
