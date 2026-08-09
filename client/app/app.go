package app

import (
	"flag"
	"fmt"
	"os"
	"time"

	clientapi "purpcmd/client/api"
	"purpcmd/client/cli"
	serverssh "purpcmd/server/ssh"
)

func Run(args []string) error {
	flags := flag.NewFlagSet("purpc", flag.ContinueOnError)
	serverURL := flags.String("server", "http://127.0.0.1:8080", "teamserver base URL")
	token := flags.String("token", os.Getenv("PURPCMD_TOKEN"), "operator bearer token (or PURPCMD_TOKEN)")
	insecureTLS := flags.Bool("insecure-tls", false, "skip TLS certificate verification")
	sshKey := flags.String("ssh-key", "template/key/id_ecdsa", "interactive SSH private key")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *token == "" {
		return fmt.Errorf("operator token is required; use -token or PURPCMD_TOKEN")
	}
	serverssh.PrivateKeyPath = *sshKey
	client := clientapi.New(clientapi.Config{
		URL:         *serverURL,
		Token:       *token,
		Timeout:     30 * time.Second,
		InsecureTLS: *insecureTLS,
	})
	defer client.Close()
	return cli.New(client).Run()
}
