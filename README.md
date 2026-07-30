# gnar

gnar publishes a local HTTP service to the internet and turns incoming traffic into an interactive terminal workspace.

Run `gnar` with no arguments and it finds what is already listening, names each service, and publishes the one you pick:

![gnar discovering and identifying local services](docs/discover.svg)

Every request through the endpoint lands in an inspector you can read, filter, and replay:

![the gnar request inspector showing captured requests and a response body](docs/inspect.svg)

Both images are generated from a real run by `docs/screenshot.py`; regenerate them with `uv run --with pyte --with requests docs/screenshot.py` after `cargo build --release`.

This document is the single source of truth for the product and its architecture. If implementation and this document disagree, update the design decision here before changing behavior.

## Install

Install the latest release on macOS or Linux:

```console
$ curl --proto '=https' --tlsv1.2 -LsSf https://raw.githubusercontent.com/abcdlsj/gnar/master/install.sh | sh
```

The installer supports Apple Silicon Macs and x86_64 or arm64 Linux machines. It verifies the downloaded archive and installs `gnar` to `$HOME/.local/bin`. Set `GNAR_INSTALL_DIR` to choose another directory or `GNAR_VERSION` to install a specific release, such as `GNAR_VERSION=1.0.0`.

## Product promise

The shortest path is `gnar` with no arguments.

gnar should require no account, configuration file, protocol choice, or self-hosted server for a first session. It discovers a local HTTP service, connects to the default public edge, assigns an ephemeral HTTPS URL, and opens the request inspector. The client defaults to `https://edge.gnar.dev`; operating that hosted edge is deployment work outside this repository's current local acceptance scope.

The explicit form remains available:

```console
$ gnar 3000
$ gnar http://127.0.0.1:3000
$ gnar 3000 --name checkout
```

The product is built around three moments:

1. Start without thinking: `gnar` discovers and publishes the likely local service.
2. Debug without changing tools: inspect, filter, copy, export, and replay requests in the terminal.
3. Share without explaining infrastructure: copy a trustworthy HTTPS URL that works immediately.

The initial audience is developers testing webhooks, OAuth callbacks, mobile clients, local APIs, demos, and AI-generated applications.

## Development quick start

Build the single binary:

```console
$ cargo build
```

Run a local edge in one terminal:

```console
$ target/debug/gnar serve
```

Publish a local application in another terminal:

```console
$ target/debug/gnar 3000 --edge http://127.0.0.1:8910
```

The local edge uses path routing and prints a URL such as `http://127.0.0.1:8910/t/warm-panda-42`. A deployed edge configured with `--base-domain` uses subdomain routing. `GNAR_EDGE` can set the client edge without repeating the flag.

## Product boundaries

The first release supports HTTP and HTTPS applications only.

It does not include TCP or UDP forwarding, a plugin system, a web dashboard, multiple simultaneous tunnels in one client, a local daemon, detached sessions, or persistent request bodies. These can only be added after the default single-tunnel experience is complete.

`tenant`, `agent`, `control plane`, and transport details are internal concepts. They must not appear in the normal user journey.

## User experience

### Discover and connect

With no target, gnar searches for a local HTTP service. Discovery uses, in order:

1. Project metadata such as package scripts and framework configuration.
2. Listening ports owned by the current user, with the process that owns each port.
3. HTTP probes against a small set of common development ports.

Discovery must be bounded and fast. A single confident result is selected automatically. Multiple plausible results are presented as an inline prompt that draws in place on the current line, never a full-screen or centered dialog:

```console
$ gnar
Searching for a local HTTP service
Found 3 local services
› 1 Next.js    :3000  Acme Checkout
  2 Vite       :5173  Infer Lab
  3 Ollama    :11434  ollama
  ↑↓ select · 1-3 jump · enter publish · esc cancel
```

The prompt occupies a fixed number of lines. A longer list scrolls inside that window rather than growing without bound, keeps the selection surrounded by context, and reports its position:

```console
Found 13 local services
   5 JSON API  :38324  Clash Party
   6 Express   :51741  Superset · HTTP 404
   7 Rocket    :42950  ApifoxAppAgent · HTTP 404
   8 Superset  :48132  Superset · HTTP 404
   9 Superset  :48482  Superset · HTTP 404
› 10 JSON API  :57224  node · HTTP 404
  11 JSON API  :59810  node · HTTP 406
  ↑↓ select · 1-9 jump · enter publish · esc cancel · 10 of 13
```

An edge row dims when it borders hidden entries, so the window reads as a slice of a longer list rather than the whole of a short one. Only the truncated side dims: a list scrolled to its end does not suggest more below.

Choosing collapses the prompt to the decision, so the scrollback keeps one line instead of a cleared screen:

```console
✓ Next.js  :3000
```

The prompt writes to stderr, leaving stdout clean for redirection. It never enters the alternate screen, so a cancelled run leaves the terminal exactly as it was. No result produces a concise diagnostic and examples of explicit targets.

### Identify what is listening

A port number is not an answer. Discovery names each candidate so the choice is obvious without opening a browser. Identification combines three independent signals:

- the response body, which carries framework markers such as `__NEXT_DATA__`, `/@vite/client`, or `__NUXT__`;
- the `Server` and `X-Powered-By` headers, which name runtimes such as Gunicorn, Uvicorn, Werkzeug, Puma, or Express;
- the owning process name from the listener scan, and the project metadata hint for the current directory.

The strongest available signal wins, so a framework marker outranks a project hint, which outranks a bare runtime name. When nothing identifies the service, its response shape gives `JSON API` or `web app` rather than a generic label. The page `<title>` becomes a secondary detail, which is what usually distinguishes two Vite servers from each other.

A developer machine listens on far more ports than it serves. Discovery hides only what cannot be published: gnar's own edge, and operating-system services such as AirPlay. Everything else stays in the list. A port that answers `/` with an error is still a legitimate target, because an API that returns 404 at its root is normal, so those candidates rank lower but are never dropped. Ranking decides what is easy to reach; it does not decide what the developer is allowed to see.

A provided integer means `http://127.0.0.1:<port>`. A provided URL is preserved, including its base path.

### Default command surface

```text
gnar [target]                 discover or publish a local HTTP service
gnar login                    sign in to an edge and store its token
gnar logout                   forget the stored token for an edge
gnar whoami                   show the signed-in account for an edge
gnar release <name>           give up a reserved name
gnar serve                    run a self-hosted edge
gnar version                  print version information
```

The client defaults to the public edge and can be overridden by `GNAR_EDGE` or `--edge`. An edge given as a bare `host:port` is read as `http://`, and a non-HTTP scheme is refused during argument parsing rather than at connection time. Self-hosting is an advanced path and must not complicate the default command.

Until the hosted edge is deployed, the default target is unreachable. That failure says so and points at `gnar serve` instead of surfacing a resolver error.

Signing in is optional and never required to publish. Anonymous use stays the default path and stays stateless. Signing in buys exactly two things: names that stay yours, and higher quotas.

Common flags:

```text
--name <name>                 request a readable endpoint name
--edge <url>                  use a different edge; a bare host:port means http://
--no-tui                      use streaming plain output
--json                        emit machine-readable events
```

Flags should be added only when the behavior cannot be inferred safely or configured interactively.

### Sign in

`gnar login` uses a device authorization grant, so the terminal never handles a password and never needs a callback listener:

```console
$ gnar login
Open https://edge.gnar.dev/device and enter code  WDJB-MJHT
Waiting for approval…
✓ Signed in as alice
```

The client polls the edge until the code is approved, expired, or denied. It then stores one token per edge in the platform configuration directory, readable only by the owning user. `gnar whoami` reports the account for an edge; `gnar logout` removes that token.

The edge is its own authorization server. It issues device codes, serves the verification page that approves them, and mints account tokens. A self-hosted edge therefore supports the full login flow with no external identity provider, which also keeps the flow verifiable in local acceptance tests. A hosted deployment may put a real identity provider behind the same verification page without changing the client.

Accounts are off unless the operator turns them on. On first run in a terminal, `gnar serve` asks once:

```console
$ gnar serve
Who may use this edge?
› 1 Anyone may publish    no accounts, no reserved names
  2 Require an account    accounts, reserved names, higher quotas
  ↑↓ select · 1-2 jump · enter confirm · esc cancel
```

Choosing accounts asks for the approval secret that gates the verification page. Leaving it blank generates a transcribable passphrase and prints it once:

```console
✓ Require an account
  Approval secret (enter to generate one)
  ›
  generated  fi9jj-b5f6t-c8wr3-hmv3r
  Save it now; this edge will not show it again.
```

The secret is not persisted. It is read at startup from the answer to this question, `--approval-secret`, or `GNAR_APPROVAL_SECRET`, so rotating it is a restart with a new value; existing tokens keep working because the secret only governs creating accounts.

The question is skipped whenever the answer is already known or cannot be asked: `--approval-secret` and `GNAR_APPROVAL_SECRET` enable accounts directly, `--anonymous-only` declines them, and a non-interactive start (CI, systemd, redirected output) serves anonymous tunnels rather than blocking on a prompt.

When accounts are off, the device endpoints and verification page return 404 and no account can exist. Publishing is unaffected.

Tokens are random secrets shown to the client once. The edge stores only a hash, so a stolen database cannot be replayed as a login. A token identifies an account; it carries no other authority.

### Terminal workspace

When stdout and stdin are terminals, a successful connection opens the TUI. In redirected, CI, dumb-terminal, or `--no-tui` environments, the same application events render as stable plain output. `--json` renders newline-delimited JSON.

The primary screen prioritizes the public URL and live requests:

```text
 gnar  ● online                                     12/min · 37 captured

 ↗  https://warm-panda.gnar.dev
 ↘  http://127.0.0.1:3000
 REQUESTS ──────────────────────────────────────────────────── following
› GET        /api/users                        200            18ms
  POST       /webhooks/github                  500            42ms
  GET        /health                           200             3ms
 RESPONSE  GET /api/users ────────────────────────────── tab → request
 content-type: application/json

 {"users": []}

 ↑↓ select · tab req/res · / filter · r replay · e curl · c copy
```

The TUI is a request inspector, not a decorated log viewer. Its essential interactions are:

- select and inspect an exchange;
- view request and response headers and bodies;
- filter by method, path, or status;
- hold the list without stopping capture;
- scroll a long body within the inspector;
- replay a captured request against the local target;
- export a request as a curl command;
- copy or open the public URL;
- show connection loss and recovery without destroying the request list.

Selection is the only navigation state. The newest exchange stays selected while the list follows; moving off the newest row holds the list so arriving traffic never moves the selection out from under the cursor. Returning to the newest row resumes following.

The layout uses terminal-relative regions and bounded columns. The request list grows with its content up to a fraction of the viewport, so the inspector keeps usable height. Color communicates status but is never the only signal. Keyboard hints stay visible, drop from the end when the width cannot hold them, and never overlap transient notices. Notices expire on their own; a lost connection stays visible until it recovers.

Body inspection defaults to text-like content with size and secret-aware redaction limits. Captured bodies live in bounded client memory and are discarded when the process exits.

### Failure behavior

Errors should lead to an action. Discovery, invalid target, edge connectivity, local connectivity, and upstream HTTP failures are distinct states with distinct messages.

The client reconnects transient edge failures with bounded exponential backoff and reuses its endpoint name. Quitting restores the terminal immediately.

## System model

There are three runtime participants:

```text
public caller ──HTTPS──> edge <──tunnel── local gnar ──HTTP──> local app
```

- The caller is a browser, webhook provider, mobile device, collaborator, or automated client.
- The edge owns public routing, TLS termination, limits, and tunnel sessions.
- The local gnar process forwards requests, captures exchanges, and owns the interactive UI.

The normal user runs only the local process once the default hosted edge is deployed. Advanced users and local development may run the same binary as a self-hosted edge with `gnar serve`.

### Domain language

`Tunnel` is one public endpoint mapped to one local HTTP target.

`Session` is one live connection between a local process and an edge. A tunnel may receive a replacement session during reconnection.

`Exchange` is one public HTTP request and its resulting HTTP response or forwarding error.

`Endpoint` is the public name and routing ownership of a tunnel. Ephemeral endpoints expire; authenticated endpoints may be reserved.

`Event` is an immutable application-level change consumed by a user-facing renderer.

Names should use this language. New synonyms require a design change here.

## Architecture

gnar is rewritten in Rust as one binary and initially one crate. Modules follow product responsibilities rather than technical layers:

```text
src/
  main.rs       process entry and shutdown
  cli.rs        command and target parsing
  app.rs        client orchestration
  discover.rs   local service discovery
  tunnel.rs     tunnel connection and local forwarding
  protocol.rs   edge transport messages
  output.rs     plain and JSON event renderers
  ui.rs         interactive selection and request inspector
  edge.rs       public HTTP routing and tunnel sessions
  store.rs      SQLite state and migrations
```

This is a direction, not a requirement to create empty modules. Code stays together until it has a cohesive reason to move.

Network and UI runtimes communicate through bounded channels of owned messages. The UI never reads transport state directly. Lifecycle behavior is shared by interactive and headless modes.

Interfaces are introduced at real boundaries such as persistence and transport, not in anticipation of hypothetical implementations. Prefer concrete types until a second implementation or a test boundary makes an interface useful.

### Tunnel transport

The edge accepts public HTTP requests and forwards them over one long-lived WebSocket connection per session. WebSocket is an internal transport choice; the exposed product remains HTTP-first. Authentication and endpoint ownership are not part of the anonymous first slice.

One connection multiplexes exchanges using compact binary frames:

```text
request start
request body chunk
request end
response start
response body chunk
response end
cancel
```

Every exchange has an opaque identifier. Bodies are streamed with bounded buffers and backpressure rather than encoded into JSON or buffered as a whole. Header values remain byte-safe. The edge can cancel oversized or timed-out requests. Limits are enforced for headers, chunks, request bodies, concurrent exchanges, and time to the local response head.

The protocol is versioned at connection setup. Incompatible peers reject the connection. The first protocol version optimizes correctness and observability over extensibility.

### Public routing

The hosted edge is expected to terminate TLS and route `<name>.gnar.dev` to an active session. Ephemeral names are adjective-animal identifiers with random entropy. An inactive or unknown endpoint returns a small branded HTTP response rather than an infrastructure error.

Self-hosted mode accepts a base domain and uses the same endpoint model. TLS termination and wildcard DNS belong to the operator's reverse proxy and do not leak into the client workflow.

### SQLite state

SQLite is the only durable store in the first release. The edge keeps hot session routing in memory and writes durable ownership and lifecycle state to SQLite.

The current database stores:

- ephemeral endpoint names and expiry;
- tunnel and session lifecycle metadata;
- aggregate usage and request metadata needed for limits and diagnostics;
- schema migration state.

The database does not store request or response bodies. Headers are not persisted by default. Live routing channels, pending exchanges, and reconnect timers are in memory.

SQLite access runs behind one small store boundary. Migrations are embedded and applied transactionally at edge startup. A dedicated database worker owns the connection, which keeps async request handling free of blocking SQLite calls and respects SQLite's single-writer model. WAL mode, foreign keys, and a busy timeout are enabled explicitly.

Initial logical tables are:

```text
accounts(id, name, created_at)
account_tokens(id, account_id, token_hash, label, created_at, last_used_at)
device_authorizations(id, device_code_hash, user_code, account_id, status, created_at, expires_at)
endpoints(id, name, kind, account_id, created_at, expires_at)
tunnel_sessions(id, endpoint_id, connected_at, disconnected_at, close_reason)
request_metrics(id, session_id, method, status, duration_ms, bytes_in, bytes_out, created_at)
request_quota(endpoint_id, minute, requests)
```

Token and device-code values are never stored, only their hashes. `endpoints.account_id` is null for anonymous endpoints, which is what distinguishes an expiring name from a reserved one.

Anonymous tunnels always receive expiring endpoints, and an anonymous `--name` is a readability preference with no ownership. An authenticated `--name` reserves the endpoint for that account: it stops expiring, and another account asking for it is refused with a message naming the conflict rather than silently receiving a different name. Reserved names are released by their owner, not by time.

### Quotas

Quotas exist so one account cannot exhaust a shared edge. They are enforced per account, with anonymous traffic treated as a single tighter tier:

```text
                        signed in     anonymous
concurrent tunnels              3             1
requests per minute           600           120
```

Concurrent tunnels are counted from live sessions. A tunnel beyond the limit is refused at handshake with a message that says which limit was hit and what to do. Request rate is counted per tunnel in one-minute buckets; traffic beyond the limit receives HTTP 429 and the branded page rather than reaching the local service.

Buckets are persisted, so restarting the edge does not hand out a fresh allowance. Limits are configurable on a self-hosted edge because its operator owns its capacity.

### Local state

Anonymous use is stateless and writes nothing. Signing in stores one token per edge in the platform configuration directory, keyed by edge URL so a self-hosted edge and the hosted edge can be signed in at once. The file is created with owner-only permissions and holds no other state. Removing it is equivalent to `gnar logout`.

### Dependencies

Third-party libraries are preferred when they provide a well-maintained, focused implementation of non-product infrastructure. The expected foundation is:

- Tokio for the async runtime;
- Axum and Hyper for the edge HTTP server;
- Reqwest for local HTTP probing, forwarding, and replay;
- Tokio Tungstenite for WebSocket transport;
- Clap for command parsing;
- Serde for configuration and protocol metadata;
- Ratatui and Crossterm for terminal interaction;
- Rusqlite for SQLite;

Dependency count is not a goal by itself. Each dependency must remove meaningful code, have a healthy maintenance story, and stay behind a narrow local boundary. Avoid overlapping libraries that solve the same problem.

## Security and privacy

The edge treats all forwarded traffic as untrusted. It validates routing metadata, bounds queues and request payloads, and applies response-head timeouts. An anonymous `--name` provides readability only; ownership requires an account.

Tokens are compared in constant time against stored hashes. A token that does not match is refused without revealing whether the account exists. Device codes are single-use and expire; an approved code cannot be redeemed twice. Tokens and device codes never appear in logs, in SQLite, or in error messages.

The hosted edge must use TLS. The built-in edge binds to loopback by default. Binding a non-loopback address requires `--allow-public-bind`, because doing so exposes the tunnel handshake and the device verification page to the network; the edge refuses to start otherwise rather than quietly listening in the open. Approving a device code always requires the approval secret, and an edge without one cannot create accounts at all, so an exposed verification page can never mint an account on its own.

The approval secret is a single shared static value with no expiry, per-operator identity, or attempt throttling. It is proportionate to self-hosting and local development, not to a shared hosted edge; that deployment should place a real identity provider behind the verification page.

The TUI redacts common credential headers and JSON fields when displaying or exporting requests. Secrets and bodies never enter diagnostic logs or SQLite.

Replay targets the configured local service, not the public endpoint, and visibly marks the resulting exchange to prevent accidental replay loops.

## Delivery order

Work proceeds in vertical slices that remain runnable:

- [x] Rust binary, explicit target parsing, local health probe, and plain or JSON event output.
- [x] Initial project-aware discovery for common development servers and ports.
- [x] Edge session creation and streamed, multiplexed HTTP exchanges through the tunnel.
- [x] Anonymous ephemeral endpoints and current-user listening-port discovery.
- [x] Request-list TUI with detail inspection, copy, filtering, and reconnect states.
- [x] Replay and curl export with bounded body capture and redaction.
- [x] SQLite endpoint lifecycle, migrations, request metrics, and edge restart recovery.
- [x] Login, stable names, quotas, and self-hosted operational hardening.

Pure behavior receives unit coverage. The tunnel acceptance test starts an edge, a local upstream, and a client, then verifies forwarding, streaming, persistence, and reconnection. Protocol limits, cancellation, terminal fallback, and migration behavior receive proportionate integration coverage before a hosted release.

## Definition of simple

A design is simpler when it reduces what a user or maintainer must understand, not merely when it has fewer lines.

The default path remains `gnar`. Advanced behavior stays behind progressive disclosure. Product concepts map to code names. State has one owner. Queues and lifetimes are explicit. Errors preserve context and propose a next action. A feature that weakens these properties must justify itself here before implementation.
