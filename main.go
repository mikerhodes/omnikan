package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/mikerhodes/omnikan/internal/omnifocus"
)

const boardRefreshInterval = 10 * time.Minute

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	flags := flag.NewFlagSet("omnikan", flag.ContinueOnError)
	projectName := flags.String("project", "", "OmniFocus project name")
	addr := flags.String("addr", "localhost:8080", "listen address")
	dynamicAssets := flags.Bool("dynamic", false,
		"use assets/ rather than embedded assets")

	if err := flags.Parse(args); err != nil {
		return err
	}

	host, port, _ := net.SplitHostPort(*addr)
	if host == "" {
		host = "localhost"
	}
	fullAddr := net.JoinHostPort(host, port)

	if *projectName == "" {
		return errors.New("-project is required")
	}
	id, err := omnifocus.ProjectID(*projectName)
	if err != nil {
		return fmt.Errorf("project %q not found in OmniFocus: %w", *projectName, err)
	}
	log.Printf("resolved project %q -> %s", *projectName, id)

	log.Printf("Loading board from OmniFocus...")
	cache := &writeThroughCache{
		board:     &kanbanBoard{},
		tasks:     map[string]*cachedTask{},
		projectID: id,
	}
	if err := cache.refresh(); err != nil {
		return fmt.Errorf("initial board load failed: %w", err)
	}
	go func() {
		for range time.Tick(boardRefreshInterval) {
			if err := cache.refresh(); err != nil {
				fmt.Fprintf(os.Stderr, "board refresh error: %v", err)
			}
		}
	}()

	srv := newServer(cache, *dynamicAssets)
	httpServer := &http.Server{
		Addr:    fullAddr,
		Handler: srv,
	}
	go func() {
		log.Printf("listening on http://%s\n", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "error listening and serving: %s\n", err)
		}
	}()
	var wg sync.WaitGroup
	wg.Go(func() {
		<-ctx.Done()
		shutdownCtx := context.Background()
		shutdownCtx, cancel := context.WithTimeout(shutdownCtx, 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "error shutting down http server: %s\n", err)
		}
	})
	wg.Wait()

	return nil
}
