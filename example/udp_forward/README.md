# UDP Forward

1. Start sample UDP server
```sh
go run example/udp_forward/echo.go
```

2. Start <Local> server
```sh
DEBUG=true ./gnar server -t 'test'
```

3. Start <Local> client
```sh
DEBUG=true ./gnar client -s 127.0.0.1:8910 -l 3000 -u 9100 -t 'test' -y 'udp'
```

4. Test
```sh
nc -u localhost 9100
```

```
» ~ nc -u localhost 9100
cs
time: Sat Oct 21 12:46:48 2023, message: cs
cs
time: Sat Oct 21 12:46:49 2023, message: cs
```

