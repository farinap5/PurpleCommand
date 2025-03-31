package listener

import (
	"context"
	"fmt"
	"net/http"
	"purpcmd/server/log"
	"time"
)

func (l *Listener) StartHTTP() {
	if l.SC.running {
		println("server is running")
	}

	serverMux := http.NewServeMux()
	serverMux.HandleFunc("/", l.root)

	l.SC.server = &http.Server{
		Addr:    l.Host + ":" + l.Port,
		Handler: serverMux,
	}

	l.SC.running = true
	l.SC.wg.Add(1)

	go func() {
		defer l.SC.wg.Done()
		log.AsyncWriteStdoutInfo(fmt.Sprintf("Starting server at %s\n", l.Host + ":" + l.Port))

		if err := l.SC.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("HTTP server error: %v\n", err)
		}
		fmt.Println("Server stopped.")
	}()
}

func (l *Listener) StopHTTP() {
	if !l.SC.running {
		fmt.Println("Server is not running.")
		return
	}

	fmt.Println("Stopping server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := l.SC.server.Shutdown(ctx)
	if err != nil {
		fmt.Printf("Error shutting down server: %v\n", err)
	}

	l.SC.running = false
	l.SC.wg.Wait()
}