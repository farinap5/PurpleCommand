package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	client "purpcmd/client/app"

	"purpcmd/internal/encrypt"
	"purpcmd/server/db"
	"purpcmd/server/implant"
	"purpcmd/server/implantbuilder"
	"purpcmd/server/listener"
	"purpcmd/server/loot"
	"purpcmd/server/lua"
	"purpcmd/server/runtimeevents"
	"purpcmd/teamserver/config"
	"purpcmd/teamserver/events"
	teamserver "purpcmd/teamserver/server"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Subcommands Expected: 'server' or 'client'.")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "client":
		if err := client.Run(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "purpc:", err)
			os.Exit(1)
		}
	case "server":
		startServer(os.Args[2:])
	default:
		fmt.Printf("Unknown subcommand: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func startServer(a []string) {
	configuration, err := config.Parse(a)
	if err != nil {
		fmt.Fprintln(os.Stderr, "teamserver configuration:", err)
		os.Exit(2)
	}
	db.DatabasePath = configuration.Database
	loot.StorageDir = configuration.LootDir
	if err := db.CheckDB(); err != nil {
		fmt.Fprintln(os.Stderr, "database:", err)
		os.Exit(1)
	}
	defer db.DBMS.DBConn.Close()
	if err := db.EnsureTeamserverSchema(); err != nil {
		fmt.Fprintln(os.Stderr, "teamserver schema:", err)
		os.Exit(1)
	}
	if err := encrypt.LoadServerRSAKey(configuration.RSAKey); err != nil {
		fmt.Fprintln(os.Stderr, "RSA key:", err)
		os.Exit(1)
	}

	eventBus := events.New()
	runtimeevents.SetPublisher(func(eventType string, value any) {
		_, _ = eventBus.Publish(eventType, value)
	})
	if err := implant.RestoreFromDB(); err != nil {
		fmt.Fprintln(os.Stderr, "restore sessions:", err)
		os.Exit(1)
	}
	implantbuilder.ProfilesReloadFromDB()
	lua.ImplantDefinitionsReloadFromDB()
	lua.ScriptsReloadFromDB()
	if err := listener.ListenerInitFromDB(); err != nil {
		fmt.Fprintln(os.Stderr, "listeners:", err)
		os.Exit(1)
	}

	server := teamserver.New(configuration, eventBus)
	scheme := "http"
	if configuration.TLS() {
		scheme = "https"
	}
	fmt.Printf("PurpleCommand teamserver: %s://%s\n", scheme, configuration.Listen)
	if configuration.GeneratedToken {
		fmt.Printf("Generated operator token: %s\n", configuration.Token)
	}

	errorsChannel := make(chan error, 1)
	go func() { errorsChannel <- server.ListenAndServe() }()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	select {
	case <-signals:
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	case err := <-errorsChannel:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "teamserver:", err)
			os.Exit(1)
		}
	}
}
