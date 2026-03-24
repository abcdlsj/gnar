package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sync"
	"time"

	"github.com/abcdlsj/gnar/internal/httpx"
	"github.com/abcdlsj/gnar/pkg/api"
)

type Runner struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Runner {
	return &Runner{
		cfg: cfg,
		http: &http.Client{
			Timeout: cfg.RequestTimeout + 5*time.Second,
		},
	}
}

func (r *Runner) Run(ctx context.Context) error {
	return r.RunWithHooks(ctx, RunnerHooks{
		OnRegistered: func(registration *api.RegisterTunnelResponse) {
			r.printBanner(registration)
		},
		OnPollError: func(err error) {
			fmt.Fprintf(os.Stderr, "poll failed: %v\n", err)
		},
	})
}

func (r *Runner) RunWithHooks(ctx context.Context, hooks RunnerHooks) error {
	registration, err := r.register(ctx)
	if err != nil {
		if hooks.OnStopped != nil {
			hooks.OnStopped(err)
		}
		return err
	}

	if hooks.OnRegistered != nil {
		hooks.OnRegistered(registration)
	}
	defer r.unregister(context.Background(), registration.SessionID)
	defer func() {
		if hooks.OnStopped != nil {
			hooks.OnStopped(nil)
		}
	}()

	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		default:
		}

		event, err := r.poll(ctx, registration.SessionID)
		if err != nil {
			if ctx.Err() != nil {
				wg.Wait()
				return nil
			}
			if hooks.OnPollError != nil {
				hooks.OnPollError(err)
			}
			select {
			case <-ctx.Done():
				wg.Wait()
				return nil
			case <-time.After(r.cfg.PollRetryBackoff):
				continue
			}
		}

		if event.Type != api.EventHTTPRequest || event.Request == nil {
			continue
		}

		sem <- struct{}{}
		wg.Add(1)
		go func(request *api.HTTPRequestEvent) {
			defer wg.Done()
			defer func() { <-sem }()
			response := r.forward(ctx, request)
			if err := r.respond(ctx, registration.SessionID, response); err != nil {
				fmt.Fprintf(os.Stderr, "respond failed: %v\n", err)
			}
		}(event.Request)
	}
}

func (r *Runner) register(ctx context.Context) (*api.RegisterTunnelResponse, error) {
	payload := api.RegisterTunnelRequest{
		Token:   r.cfg.Token,
		Tenant:  r.cfg.Tenant,
		Name:    r.cfg.Name,
		Target:  r.cfg.TargetURL,
		Domains: r.cfg.Domains,
	}

	req, err := r.newJSONRequest(ctx, http.MethodPost, "/api/v1/agent/register", payload)
	if err != nil {
		return nil, err
	}

	resp, err := r.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, httpx.DecodeError(resp)
	}

	var registration api.RegisterTunnelResponse
	if err := json.NewDecoder(resp.Body).Decode(&registration); err != nil {
		return nil, err
	}
	return &registration, nil
}

func (r *Runner) poll(ctx context.Context, sessionID string) (api.AgentEvent, error) {
	req, err := r.newRequest(ctx, http.MethodGet, "/api/v1/agent/poll?session_id="+url.QueryEscape(sessionID), nil)
	if err != nil {
		return api.AgentEvent{}, err
	}

	resp, err := r.http.Do(req)
	if err != nil {
		return api.AgentEvent{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return api.AgentEvent{}, httpx.DecodeError(resp)
	}

	var payload api.PollResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return api.AgentEvent{}, err
	}
	return payload.Event, nil
}

func (r *Runner) respond(ctx context.Context, sessionID string, payload api.PostResponseRequest) error {
	req, err := r.newJSONRequest(ctx, http.MethodPost, "/api/v1/agent/respond?session_id="+url.QueryEscape(sessionID), payload)
	if err != nil {
		return err
	}

	resp, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return httpx.DecodeError(resp)
	}
	return nil
}

func (r *Runner) unregister(ctx context.Context, sessionID string) {
	unregisterCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := r.newRequest(unregisterCtx, http.MethodPost, "/api/v1/agent/unregister?session_id="+url.QueryEscape(sessionID), nil)
	if err != nil {
		return
	}

	resp, err := r.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
}

func (r *Runner) forward(ctx context.Context, request *api.HTTPRequestEvent) api.PostResponseRequest {
	outboundURL, err := resolveOutboundURL(r.cfg.TargetURL, request.Path, request.RawQuery)
	if err != nil {
		return localErrorResponse(request.RequestID, err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, r.cfg.RequestTimeout)
	defer cancel()

	outboundReq, err := http.NewRequestWithContext(reqCtx, request.Method, outboundURL, bytes.NewReader(request.Body))
	if err != nil {
		return localErrorResponse(request.RequestID, err)
	}

	outboundReq.Header = http.Header(httpx.CloneHeaderMap(request.Headers))
	outboundReq.Header.Set("X-Forwarded-Host", request.Host)
	outboundReq.Header.Set("X-Forwarded-Proto", request.Scheme)
	outboundReq.Header.Set("X-Forwarded-For", request.RemoteAddr)
	outboundReq.Host = request.Host

	resp, err := r.http.Do(outboundReq)
	if err != nil {
		return localErrorResponse(request.RequestID, err)
	}
	defer resp.Body.Close()

	body, err := readLimited(resp.Body, r.cfg.MaxResponseBytes)
	if err != nil {
		return localErrorResponse(request.RequestID, err)
	}

	return api.PostResponseRequest{
		RequestID:  request.RequestID,
		StatusCode: resp.StatusCode,
		Headers:    httpx.HeaderToMap(resp.Header),
		Body:       body,
	}
}

func (r *Runner) printBanner(registration *api.RegisterTunnelResponse) {
	fmt.Printf("Tunnel: %s\n", registration.Name)
	fmt.Printf("Local:  %s\n", r.cfg.TargetURL)
	fmt.Printf("Public: %s\n", registration.PublicURL)
	for _, value := range registration.URLs {
		if value == registration.PublicURL {
			continue
		}
		fmt.Printf("Alias:  %s\n", value)
	}
	fmt.Println("State:  connected")
	fmt.Println("Press Ctrl+C to stop")
}

func (r *Runner) newJSONRequest(ctx context.Context, method, endpoint string, payload any) (*http.Request, error) {
	return httpx.NewJSONRequest(ctx, r.cfg.ServerURL, method, endpoint, payload)
}

func (r *Runner) newRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	return httpx.NewRequest(ctx, r.cfg.ServerURL, method, endpoint, body)
}

func resolveOutboundURL(target, requestPath, rawQuery string) (string, error) {
	base, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	if requestPath == "" {
		requestPath = "/"
	}

	resolved := *base
	resolved.Path = joinURLPath(base.Path, requestPath)
	resolved.RawPath = resolved.Path
	resolved.RawQuery = mergeRawQuery(base.RawQuery, rawQuery)
	return resolved.String(), nil
}

func joinURLPath(basePath, requestPath string) string {
	if basePath == "" || basePath == "/" {
		if requestPath == "" {
			return "/"
		}
		return requestPath
	}

	if requestPath == "" || requestPath == "/" {
		return basePath
	}

	return path.Join(basePath, requestPath)
}

func mergeRawQuery(baseQuery, requestQuery string) string {
	switch {
	case baseQuery == "":
		return requestQuery
	case requestQuery == "":
		return baseQuery
	default:
		return baseQuery + "&" + requestQuery
	}
}

func readLimited(body io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(body)
	}
	reader := io.LimitReader(body, limit+1)
	buf, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > limit {
		return nil, fmt.Errorf("upstream response exceeds max-response-bytes")
	}
	return buf, nil
}

func localErrorResponse(requestID string, err error) api.PostResponseRequest {
	return api.PostResponseRequest{
		RequestID:  requestID,
		StatusCode: http.StatusBadGateway,
		Headers: map[string][]string{
			"Content-Type": {"text/plain; charset=utf-8"},
		},
		Body: []byte(err.Error()),
	}
}
