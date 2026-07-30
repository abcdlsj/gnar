# gnar

gnar publishes a local HTTP service to the internet and shows incoming requests in an interactive terminal inspector.

Run `gnar` without a target to discover local services:

![gnar discovering and identifying local services](docs/discover.svg)

Inspect, filter, replay, and export requests from the terminal:

![the gnar request inspector showing captured requests and a response body](docs/inspect.svg)

## Install

Install the latest release on macOS or Linux:

```console
$ curl --proto '=https' --tlsv1.2 -LsSf https://raw.githubusercontent.com/abcdlsj/gnar/master/install.sh | sh
```

The installer supports Apple Silicon Macs and x86_64 or arm64 Linux machines. It installs `gnar` to `$HOME/.local/bin`.

Set `GNAR_INSTALL_DIR` to use another directory. Set `GNAR_VERSION` to install a specific release:

```console
$ curl --proto '=https' --tlsv1.2 -LsSf https://raw.githubusercontent.com/abcdlsj/gnar/master/install.sh \
    | GNAR_VERSION=1.0.0 sh
```

## Quick start

gnar needs an edge server. Sign in to a self-hosted edge before publishing:

```console
$ gnar login --edge https://gnar.example.com
```

Start a local application, then run:

```console
$ gnar
```

You can also provide a port, URL, or endpoint name:

```console
$ gnar 3000
$ gnar http://127.0.0.1:3000
$ gnar 3000 --name checkout
```

When several local services are available, gnar asks which service to publish. After confirming that service is reachable, gnar chooses the edge as follows:

- One signed-in edge is selected automatically.
- Several signed-in edges are shown in an interactive selection.
- `--edge` or `GNAR_EDGE` selects an edge explicitly and skips the selection.
- No available edge stops the command and explains how to self-host and sign in.
- A non-interactive command with several signed-in edges requires `--edge` or `GNAR_EDGE`.

gnar does not use a public edge by default.

## Commands

```text
gnar [target]                 discover or publish a local HTTP service
gnar login                    sign in to an edge and store its token
gnar logout                   forget the stored token for an edge
gnar whoami                   show the signed-in account for an edge
gnar release <name>           release a reserved endpoint name
gnar serve                    run a self-hosted edge
gnar version                  print version information
```

Common options:

```text
--name <name>                 request a readable endpoint name
--edge <url>                  use a specific edge
--no-tui                      use streaming plain output
--json                        emit newline-delimited JSON events
```

A bare edge such as `127.0.0.1:8910` uses HTTP. Public edge servers should use HTTPS.

## Sign in

Each edge manages its own accounts. Sign in with the device flow:

```console
$ gnar login --edge https://gnar.example.com
Open https://gnar.example.com/device and enter code  WDJB-MJHT
Waiting for approval…
✓ Signed in as alice
```

gnar stores one token for each signed-in edge in the user's configuration directory. The credentials file is readable only by that user.

Signing in provides reserved endpoint names and higher quotas. Anonymous publishing remains available when the edge operator allows it.

Use the same edge when managing an account or endpoint explicitly:

```console
$ gnar whoami --edge https://gnar.example.com
$ gnar logout --edge https://gnar.example.com
$ gnar release checkout --edge https://gnar.example.com
```

## Request inspector

The interactive inspector provides these actions:

- Select a request and inspect its response.
- Switch between request and response details.
- Filter by method, path, or status.
- Scroll long bodies.
- Replay a request against the local service.
- Export a request as a curl command.
- Copy or open the public URL.

Use `--no-tui` for stable plain output. Use `--json` for newline-delimited JSON events.

## Self-host an edge

### Run the binary

Start an edge for local use:

```console
$ gnar serve
```

The interactive setup asks whether the edge allows anonymous publishing or requires accounts. Account mode uses an approval secret for the device verification page.

For a non-interactive account-enabled deployment, provide the secret explicitly:

```console
$ GNAR_APPROVAL_SECRET='replace-this-secret' \
    gnar serve \
    --listen 127.0.0.1:8910 \
    --public-url https://gnar.example.com \
    --database gnar.db
```

### Run with Docker

The published container runs as a non-root user and stores persistent state in `/data`:

```console
$ docker run -d --name gnar --restart unless-stopped \
    --env-file /path/to/gnar.env \
    -p 127.0.0.1:8910:8910 \
    -v gnar-data:/data \
    ghcr.io/abcdlsj/gnar:latest \
    serve \
    --listen 0.0.0.0:8910 \
    --public-url https://gnar.example.com \
    --database /data/gnar.db \
    --allow-public-bind
```

Put an HTTPS reverse proxy in front of the container. The proxy must preserve WebSocket upgrades.

Use `--anonymous-only` instead of an approval secret when accounts are not needed.

## Security and privacy

- The edge does not store request or response bodies.
- Diagnostic logs do not contain request bodies, response bodies, or tokens.
- The inspector redacts common credential headers and JSON fields.
- Account tokens and device codes are stored as hashes on the edge.
- The built-in edge listens on loopback by default.
- A non-loopback listener requires `--allow-public-bind`.
- Public deployments should terminate TLS at a reverse proxy.
