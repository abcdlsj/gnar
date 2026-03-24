package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/abcdlsj/gnar/internal/httpx"
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
	return httpx.NewJSONRequest(ctx, c.baseURL, method, path, payload)
}

func (c *DaemonClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	return httpx.NewRequest(ctx, c.baseURL, method, path, body)
}

func (c *DaemonClient) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return httpx.DecodeError(resp)
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
