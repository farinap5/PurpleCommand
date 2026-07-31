package listener

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"purpcmd/server/db"
	"purpcmd/server/log"
)

var (
	ErrListenerRunning    = errors.New("listener is already running")
	ErrListenerNotRunning = errors.New("listener is not running")
)

func (l *Listener) StartHTTP() error {
	l.SC.mu.Lock()
	if l.SC.running {
		l.SC.mu.Unlock()
		return ErrListenerRunning
	}

	serverMux := http.NewServeMux()
	serverMux.HandleFunc("/", l.root)
	server := &http.Server{
		Addr:    l.Host + ":" + l.Port,
		Handler: serverMux,
	}
	networkListener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		l.SC.mu.Unlock()
		return err
	}

	l.SC.server = server
	l.SC.running = true
	done := make(chan struct{})
	l.SC.done = done
	persistent := l.Persistent
	l.SC.mu.Unlock()

	if persistent {
		if err := db.DBListenerUpdateOption(l.Name, "running", "true"); err != nil {
			_ = networkListener.Close()
			l.SC.mu.Lock()
			l.SC.running = false
			l.SC.server = nil
			l.SC.done = nil
			l.SC.mu.Unlock()
			return err
		}
	}

	go func() {
		defer close(done)
		log.AsyncWriteStdoutInfo(fmt.Sprintf("Starting server at %s\n", server.Addr))

		if err := server.Serve(networkListener); err != nil && err != http.ErrServerClosed {
			fmt.Printf("HTTP server error: %v\n", err)
		}

		var persistenceErr error
		l.SC.mu.Lock()
		if l.SC.server == server {
			if l.Persistent {
				persistenceErr = db.DBListenerUpdateOption(l.Name, "running", "false")
			}
			l.SC.running = false
			l.SC.server = nil
			l.SC.done = nil
		}
		l.SC.mu.Unlock()

		fmt.Println("Server stopped.")
		if persistenceErr != nil {
			log.AsyncWriteStdoutErr(persistenceErr.Error())
		}
	}()
	return nil
}

func (l *Listener) StopHTTP() error {
	l.SC.mu.Lock()
	if !l.SC.running || l.SC.server == nil {
		l.SC.mu.Unlock()
		return ErrListenerNotRunning
	}
	server := l.SC.server
	done := l.SC.done
	l.SC.mu.Unlock()

	fmt.Println("Stopping server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shut down listener: %w", err)
	}

	<-done
	return nil
}
