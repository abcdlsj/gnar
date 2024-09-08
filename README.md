<!-- TOC start (generated with https://github.com/derlin/bitdowntoc) -->

- [Gnar](#gnar)
  - [Features](#features)
  - [Installation](#installation)
  - [Quick Start](#quick-start)
  - [Configuration](#configuration)
    - [Command line flags](#command-line-flags)
      - [Server](#server)
      - [Client](#client)
    - [Configuration Files](#configuration-files)
      - [Client Configuration (client\_config.toml)](#client-configuration-client_configtoml)
      - [Server Configuration (server\_config.toml)](#server-configuration-server_configtoml)
    - [Positional Arguments](#positional-arguments)
      - [Server](#server-1)
      - [Client](#client-1)
  - [Advanced Usage](#advanced-usage)
    - [Subdomain Proxy](#subdomain-proxy)
    - [Deploying on `fly.io`](#deploying-on-flyio)
  - [Environment Variables](#environment-variables)
    - [Server](#server-2)
    - [Client](#client-2)
  - [Trubleshooting](#trubleshooting)
  - [Contributing](#contributing)
  - [License](#license)

<!-- TOC end -->

<!-- TOC --><a name="gnar"></a>
# Gnar
A Versatile Proxy Tool with Auto-HTTPS Subdomain Support.

![run gif](assets/run.gif)

Gnar is a powerful and flexible __proxy__ tool, similar to frp, with built-in support for __Auto-HTTPS__ subdomain proxying. It's designed to be simple yet feature-rich, making it an ideal solution for developers who need a reliable and secure proxy setup.

## Features

- Simple implementation with __minimal__ third-party dependencies
- Client __graceful__ shutdown.
- Support for __TCP/UDP__ traffic forwarding
- __Subdomain proxy__ using Caddy server
- Configurable via __command-line flags__ or a __configuration file__
- __Multi-client__ forwarding support
- Token-based __authentication__ for enhanced security
- Server-side __admin panel__ for easy management
- Integration of __yamux__ for __multiplexing__ connections
- Deployable on __fly.io__

## Installation

```
git clone https://github.com/abcdlsj/gnar
make
```

## Quick Start

1. Start the server:
   with positional argument:
   ```bash
   gnar server 8910
   ```

   with flag:
   ```bash
   gnar server -p 8910
   ```

   Or using a configuration file:
   ```bash
   gnar server -c server_config.toml
   ```

2. Start the client:
   with positional argument:
   ```bash
   gnar client localhost:8910 3000:9001
   ```

   with flag:
   ```bash
   gnar client -s localhost:8910 -p 3000:9001
   ```

   Or using a configuration file:
   ```bash
   gnar client -c client_config.toml
   ```

3. Start a sample service:
   ```bash
   python3 -m http.server 3000
   ```

4. Access your service at `localhost:9001`

## Configuration

Gnar supports both command-line flags and configuration files. Here's a sample client configuration:

### Command line flags

#### Server

```
Run gnar server with optional port argument

Usage:
  gnar server [port] [flags]

Flags:
  -a, --admin-port int   admin server port
  -c, --config string    config file
  -D, --domain string    domain name
  -d, --domain-tunnel    enable domain tunnel
  -h, --help             help for server
  -m, --multiplex        multiplex client/server control connection
  -p, --port int         server port (default 8910)
  -t, --token string     token
```

#### Client

```
Run gnar client with optional server address and port mapping

Usage:
  gnar client [server-addr] [local-port:remote-port] [flags]

Flags:
  -c, --config string        config file
  -h, --help                 help for client
  -m, --multiplex            multiplex client/server control connection
  -n, --proxy-name string    proxy name
  -y, --proxy-type string    proxy transport protocol type (default "tcp")
  -s, --server-addr string   server addr (default "localhost:8910")
      --speed-limit string   speed limit
  -d, --subdomain string     subdomain
  -t, --token string         token
```

### Configuration Files

#### Client Configuration (client_config.toml)

```toml
server-addr = "localhost:8910"
token = "abcdlsj" # optional
multiplex = true # optional, if true will use yamux to multiplex the connection

[[proxys]]
proxy-name = "python_http_file_service" # optional
subdomain = "python3-http" # optional, if not set, will generate a random subdomain prefix
local-port = 3000
remote-port = 9001
speed-limit = "100kb" # optional, if not set, will not limit speed
proxy-type = "tcp"

[[proxys]]
local-port = 3001
remote-port = 9002
proxy-type = "tcp"
```

#### Server Configuration (server_config.toml)

```toml
port = 8910
admin-port = 8911
domain-tunnel = false
domain = "example.com"
# token = "abcdlsj" # optional
multiplex = false
```

Server admin panel:
![server admin screenshot](assets/server_admin_screenshot.png)

### Positional Arguments

#### Server

The server command accepts an optional port number as a positional argument:

```bash
gnar server [port]
```

If not provided, the default port (8910) or the port specified in the configuration file will be used.

#### Client

The client command accepts two optional positional arguments:

```bash
gnar client [server-addr] [local-port:remote-port]
```

1. `server-addr`: The address of the gnar server (e.g., "localhost:8910")
2. `local-port:remote-port`: The local and remote port mapping (e.g., "3000:9001")

If these arguments are not provided, the values from the configuration file or default values will be used.

## Advanced Usage

### Subdomain Proxy

1. Set up your domain's DNS records:
   ```
   A *.example.com <your server ip>
   A example.com <your server ip>
   ```

2. Start the Caddy server:
   ```bash
   caddy run --config <gnar path>/server/caddy.json
   ```

3. Run the Gnar server with domain tunnel enabled:
   ```bash
   gnar server 8910 -D example.com -d
   ```

4. Start the client with a custom subdomain:
   ```bash
   gnar client localhost:8910 3000:9001 -d myapp
   ```

### Deploying on `fly.io`

Gnar can be easily deployed on <https://fly.io>.

You can edit `entrypoint.sh` to start your own server **you need to special set forward port.**

Example:
```toml
# See https://fly.io/docs/reference/configuration/ for information about how to use this file.
app = "xxxx"
primary_region = "hkg"

[build]

# Control
[[services]]
  internal_port = 8910
  protocol = "tcp"

  [[services.ports]]
    port = 8910
  
# Admin
[[services]]
  internal_port = 8911
  protocol = "tcp"

  [[services.ports]]
    handlers = ["http"]
    port = 80

  [[services.ports]]
    handlers = ["tls", "http"]
    port = 443

# Forward TCP
[[services]]
  internal_port = 9000
  protocol = "tcp"

  [[services.ports]]
    handlers = ["tls", "http"]
    port = 9000
```
This can view `xxxx.fly.dev:9000` and then view your own internal server.

## Environment Variables

Gnar uses Viper to manage configuration, which allows for setting options via environment variables. The following environment variables are supported:

### Server

- `GNAR_PORT`: Server port
- `GNAR_ADMIN_PORT`: Admin server port
- `GNAR_DOMAIN_TUNNEL`: Enable domain tunnel (true/false)
- `GNAR_DOMAIN`: Domain name
- `GNAR_TOKEN`: Authentication token
- `GNAR_MULTIPLEX`: Enable connection multiplexing (true/false)

### Client

- `GNAR_TOKEN`: Authentication token
- `GNAR_MULTIPLEX`: Enable connection multiplexing (true/false)

Environment variables take precedence over configuration files and command-line flags. To use an environment variable, prefix the uppercase option name with `GNAR_`. For example, to set the server port:

```bash
export GNAR_PORT=8080
gnar server
```

This will start the server on port 8080, regardless of the default value or any value specified in a configuration file.

## Trubleshooting

1. subdomain proxy not work

  make sure you have set the dns record to your server ip. 
  if you use cloudflare, need to set dns_key in caddy.json.

## Contributing

We welcome contributions to Gnar! Please read our [Contributing Guidelines](CONTRIBUTING.md) for more information on how to get started.

## License

Gnar is released under the [MIT License](LICENSE).