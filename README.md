# gnar

gnar is an HTTP-first local service publishing tool.

## Quick start

Start the edge:

```bash
gnar server \
  --listen :8910 \
  --public-url http://127.0.0.1:8910
```

Expose a local app:

```bash
gnar http 3000 --server http://127.0.0.1:8910 --name demo
```

Open the generated path URL:

```text
http://127.0.0.1:8910/t/default/demo
```

Bind a custom domain:

```bash
gnar server \
  --listen :8910 \
  --public-url https://edge.example.com \
  --manage-token manage-secret \
  --agent-credential default=agent-secret \
  --allow-domain-suffix example.com

gnar http 3000 \
  --server https://edge.example.com \
  --name demo \
  --domain demo.example.com \
  --token agent-secret
```

Run a local daemon and start a detached tunnel:

```bash
gnar agent serve \
  --listen 127.0.0.1:7777 \
  --state-file ~/.gnar/agent-state.json

gnar http 3000 \
  --server http://127.0.0.1:8910 \
  --agent-url http://127.0.0.1:7777 \
  --detach \
  --name demo
```

## Model

- `gnar server` runs the control plane and the public HTTP edge.
- `gnar http` registers a tunnel and forwards public HTTP requests to a local upstream.
- `gnar agent serve` runs a local daemon so tunnels can outlive the foreground CLI process.
- `gnar agent serve` persists managed tunnels and restores them after restart.
- `gnar ls`, `gnar inspect`, and `gnar logs` read the live tunnel state from the server.
- `gnar doctor` checks server reachability and optional local target readiness.
- Every tunnel is namespaced by tenant and gets a path URL at `/t/<tenant>/<name>`.
- Custom domains are supported in the first version.

## Manage

List active tunnels:

```bash
gnar ls --server http://127.0.0.1:8910
```

List only one tenant:

```bash
gnar ls --server http://127.0.0.1:8910 --tenant default
```

Inspect one tunnel:

```bash
gnar inspect demo --server http://127.0.0.1:8910 --tenant default
```

Show recent requests:

```bash
gnar logs demo --server http://127.0.0.1:8910 --tenant default --limit 10
```

Run diagnostics:

```bash
gnar doctor 3000 --server http://127.0.0.1:8910
```

List local daemon tunnels:

```bash
gnar agent ls --url http://127.0.0.1:7777
```

Stop a detached tunnel:

```bash
gnar stop demo --agent-url http://127.0.0.1:7777 --tenant default
```

## Flags

Server:

```bash
gnar server \
  --listen :8910 \
  --public-url http://127.0.0.1:8910 \
  --base-domain apps.example.com \
  --agent-token shared-agent-secret \
  --manage-token shared-manage-secret \
  --agent-credential team-a=team-a-agent-secret \
  --allow-domain-suffix example.com \
  --tenant-domain-suffix team-a=team-a.example.com
```

Expose a service:

```bash
gnar http 3000 \
  --server http://127.0.0.1:8910 \
  --tenant default \
  --name my-api \
  --domain api.example.com \
  --token shared-agent-secret \
  --request-timeout 30s \
  --retry-backoff 1s
```

Management API:

```bash
gnar ls \
  --server http://127.0.0.1:8910 \
  --token shared-manage-secret \
  --tenant default
```
