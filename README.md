<!-- TOC start (generated with https://github.com/derlin/bitdowntoc) -->

- [Pipe](#pipe)
  - [Install](#install)
  - [Usage](#usage)
  - [Client](#client)
  - [Server](#server)
    - [Server admin panel](#server-admin-panel)
  - [Simple Start](#simple-start)
  - [Deploy at `fly.io`](#deploy-at-flyio)
  - [Subdomain proxy](#subdomain-proxy)
  - [Trubleshooting](#trubleshooting)

<!-- TOC end -->

<!-- TOC --><a name="pipe"></a>
# Pipe

[![asciicast](https://asciinema.org/a/606328.svg)](https://asciinema.org/a/606328)
**Do not destroy the server!!!**

frp-like Tool with AutoHTTPs Subdomain Proxy

Features:
- [x] Simple implementation with minimal third-party dependencies
- [x] Support TCP/UDP traffic forward
- [x] Support for subdomain proxy using Caddy server
- [x] Can be run via command-line flags or a configuration file
- [x] Supports forwarding from multiple clients
- [x] Includes token-based authentication for added security
- [x] Server-side admin panel (currently, it's simple)
- [x] Integration of yamux for multiplexing connections
- [x] Can deploy at `fly.io`


Future Plans:

- [ ] Daemon mode for background execution
- [ ] Smooth upgrade (upgrade client/server version)
- [ ] Add metrics (bandwidths/upward and downward)
- [x] Integration of yamux for multiplexing connections
- [x] Support `UDP` traffic forward
- [x] Can deploy at `fly.io`

<!-- TOC --><a name="install"></a>
## Install

```
git clone https://github.com/abcdlsj/pipe
make
```

<!-- TOC --><a name="usage"></a>
## Usage

```
pipe is a proxy tool.

Usage:
  pipe [flags]
  pipe [command]

Available Commands:
  client      
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  server      

Flags:
  -h, --help      help for pipe
  -v, --version   version for pipe

Use "pipe [command] --help" for more information about a command.
```

<!-- TOC --><a name="client"></a>
## Client
```
Usage:
  pipe client [flags]

Flags:
  -c, --config string        config file
  -h, --help                 help for client
  -l, --local-port int       local port
  -m, --multiplex            multiplex client/server control connection
  -n, --proxy-name string    proxy name
  -u, --proxy-port int       proxy port
  -y, --proxy-type string    proxy transport protocol type (default "tcp")
  -s, --server-addr string   server addr (default "localhost:8910")
  -d, --subdomain string     subdomain
  -t, --token string         token
```

<!-- TOC --><a name="server"></a>
## Server 
Usage:
  pipe server [flags]

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

<!-- TOC --><a name="server-admin-panel"></a>
### Server admin panel

![admin panel](screenshot-server-admin.png)

<!-- TOC --><a name="simple-start"></a>
## Simple Start

Server
```
pipe server -p 8910
```

Client
```
# start a service
python3 -m http.server 3000
# start proxy
pipe client -s localhost:8910 -l 3000 -u 9001
```

view `host:9001` and you will see the service.

<!-- TOC --><a name="deploy-at-flyio"></a>
## Deploy at `fly.io`

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

<!-- TOC --><a name="subdomain-proxy"></a>
## Subdomain proxy

1. make sure you have a domain and set the dns record to your server ip.

```
A *.example.com <your server ip>
A example.com <your server ip> (`@` is ok too)
```

2. start caddy server
```
[sudo] caddy run --config <pipe path>/server/caddy.json
```

3. start pipe server with `domain-tunnel` flag
```
./pipe server -a 8911 -D <example.com> -d -p 8910
``` 

4. start pipe client
```
./pipe client -s localhost -p 8910 -l 3000 -u 9001
```

5. now you can find the subdomain in server log, like this
```
2023/07/02 09:50:16 [INFO] Tunnel created successfully, id: 3ec8f1b-9001, host: 3ec8f1b.xxx.xxx
```

6. visit `3ec8f1b.xxx.xxx` and you will see the service.


<!-- TOC --><a name="trubleshooting"></a>
## Trubleshooting

1. subdomain proxy not work
make sure you have set the dns record to your server ip. 
if you use cloudflare, need to set dns_key in caddy.json.