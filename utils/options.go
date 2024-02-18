package utils


func Usage() {
	help := `
Server usage: purpcmd server [options] ...
	-k configures the path to the private key.
	By default an embed key pair is used to authenticate the
	connection.
	Use "-k /path/to/id_rsa".

Client usage: purpcmd client [options] ...
	-ua defines the User-Agent HTTP header to use
	during the request.

	-p must be used to set the path to a public key.
	By default an embed key pair is used to authenticate the
	connection. If the server is using a custom private key,
	this option must be used to specify the pair.
	Use "-p /path/to/id_rsa.pub".

	-ps allows passing the public key right from the command
	line.
	Use "-ps 'ssh-rsa AAAAB3NzaC'".
	
Global Options:
	-a is the address to listen on or connect to.
	Use "-a 127.0.0.1:8080".
		
	-uri configures the URI where to connect or to receive 
	the websocket connection.
	Use "-uri /assets".
`
	print(help)
}