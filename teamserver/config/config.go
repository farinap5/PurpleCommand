package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"net"
	"os"
	"strings"
)

type Config struct {
	Listen         string
	Token          string
	TLSCert        string
	TLSKey         string
	RSAKey         string
	Database       string
	LootDir        string
	ScriptDir      string
	GeneratedToken bool
}

func Parse(args []string) (Config, error) {
	var configuration Config
	flags := flag.NewFlagSet("purpcmd-teamserver", flag.ContinueOnError)
	flags.StringVar(&configuration.Listen, "listen", "127.0.0.1:8080", "teamserver HTTP address")
	flags.StringVar(&configuration.Token, "token", os.Getenv("PURPCMD_TOKEN"), "operator bearer token (or PURPCMD_TOKEN)")
	flags.StringVar(&configuration.TLSCert, "tls-cert", "", "TLS certificate path")
	flags.StringVar(&configuration.TLSKey, "tls-key", "", "TLS private key path")
	flags.StringVar(&configuration.RSAKey, "rsa-key", "server.key", "implant protocol RSA private key")
	flags.StringVar(&configuration.Database, "database", "database.db", "SQLite state database")
	flags.StringVar(&configuration.LootDir, "loot-dir", "loot", "loot storage directory")
	flags.StringVar(&configuration.ScriptDir, "script-dir", "script/uploads", "uploaded Lua script directory")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if (configuration.TLSCert == "") != (configuration.TLSKey == "") {
		return Config{}, errors.New("tls-cert and tls-key must be provided together")
	}
	host, _, err := net.SplitHostPort(configuration.Listen)
	if err != nil {
		return Config{}, err
	}
	ip := net.ParseIP(host)
	loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if !loopback && configuration.TLSCert == "" {
		return Config{}, errors.New("non-loopback teamserver binding requires TLS")
	}
	if strings.TrimSpace(configuration.Token) == "" {
		if !loopback {
			return Config{}, errors.New("a bearer token is required")
		}
		token := make([]byte, 32)
		if _, err := rand.Read(token); err != nil {
			return Config{}, err
		}
		configuration.Token = base64.RawURLEncoding.EncodeToString(token)
		configuration.GeneratedToken = true
	}
	if len(configuration.Token) < 20 {
		return Config{}, errors.New("teamserver token must contain at least 20 characters")
	}
	return configuration, nil
}

func (configuration Config) TLS() bool {
	return configuration.TLSCert != ""
}
