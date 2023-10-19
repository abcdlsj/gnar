# UDP Forward

1. Start sample UDP server
```sh
go run example/udp_forward/echo.go
```

2. Start <Local> server
```sh
DEBUG=true ./pipe server -t 'test'
```

3. Start <Local> client
```sh
DEBUG=true ./pipe client -s 127.0.0.1 -p 8910 -l 3000 -u 9100 -t 'test' -y 'udp'
```

4. Test
```sh
nc -u localhost 9100
```