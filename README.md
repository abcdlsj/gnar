# Gpipe

frp like tool.

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
  -h, --help   help for gpipe

Use "gpipe [command] --help" for more information about a command.
```

## Example

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

**Now can view `host:9001`**