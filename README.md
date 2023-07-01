# gpipe

frp like tool, but with autohttps subdomain proxy.

## feature
- [x] simple code (less than 1000 lines) 
- [x] support subdomain proxy (with caddy)
- [x] run with cmd flag or config file
- [ ] password sign check
- [ ] server side admin panel (currently already have simple pannel)
- [ ] daemon mode

## Usage

```
Usage:
  gpipe [flags]
  gpipe [command]

Available Commands:
  client      
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  server      

Flags:
  -h, --help      help for gpipe
  -v, --version   version for gpipe

Use "gpipe [command] --help" for more information about a command.
```

## client
```
Usage:
  gpipe client [flags]

Flags:
  -c, --config string        config file
  -u, --forward-port int     forward port
  -h, --help                 help for client
  -l, --local-port int       local port
  -s, --server-host string   server host (default "localhost")
  -p, --server-port int      server port (default 8910)
```

## server 
```
Usage:
  gpipe server [flags]

Flags:
  -a, --admin-port int   admin server port
  -c, --config string    config file
  -D, --domain string    domain name
  -d, --domain-tunnel    enable domain tunnel
  -h, --help             help for server
  -p, --port int         server port (default 8910)
```

## Simple Start

Server
```
gpipe server -p 8910
```

Client
```
# start a service
python3 -m http.server 3000
# start forward
gpipe client -s localhost -p 8910 -l 3000 -u 9001
```

view `host:9001` and you will see the service.

## Subdomain proxy

1. make sure you have a domain and set the dns record to your server ip.

```
A *.example.com <your server ip>
A example.com <your server ip> (`@` is ok too)
```

2. start caddy server
```
[sudo] caddy run --config <gpipe path>/server/caddy.json
```

3. start gpipe server with `domain-tunnel` flag
```
./gpipe server -a 8911 -D <example.com> -d -p 8910
``` 

4. start gpipe client
```
./gpipe client -s localhost -p 8910 -l 3000 -u 9001
```

5. now you can find the subdomain in server log, like this
```
2023/07/02 09:50:16 [INFO] Tunnel created successfully, id: 3ec8f1b-9001, host: 3ec8f1b.xxx.xxx
```

6. visit `3ec8f1b.xxx.xxx` and you will see the service.


## Trubleshooting

1. subdomain proxy not work
make sure you have set the dns record to your server ip. 
if you use cloudflare, need to set dns_key in caddy.json.


