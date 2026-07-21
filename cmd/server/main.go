package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/app"
)

func main() {
	cfgPath := flag.String("config", "configs/app.toml", "path to config file")
	flag.Parse()

	container := app.NewContainer()
	if err := container.Initialize(*cfgPath); err != nil {
		log.Fatalf("initialize: %v", err)
	}
	defer func() {
		if err := container.Close(); err != nil {
			log.Printf("close: %v", err)
		}
	}()

	srv := &http.Server{
		Addr:              container.Config.Server.Addr,
		Handler:           container.HTTP.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("qiwei-zhushou listening on %s", container.Config.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
	}
}
