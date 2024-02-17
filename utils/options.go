package utils

func Usage() {
	help := `
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

`
	print(help)
}