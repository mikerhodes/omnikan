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

var (
	projectIDLookup    = omnifocus.ProjectID
	initialRetryDelay  = 2 * time.Second
	maxRetryDelay      = 1 * time.Minute
	errProjectRequired = errors.New("-project is required")
)

const (
	initStateConfigError  = "configuration_error"
	initStateDegraded     = "degraded"
	initStateInitializing = "initializing"
	initStateReady        = "ready"
	initStateRetrying     = "retrying"
)

type initializationStatus struct {
	State       string     `json:"state"`
	Error       string     `json:"error,omitempty"`
	NextRetry   *time.Time `json:"nextRetry,omitempty"`
	Recoverable bool       `json:"recoverable"`
	RetryCount  int        `json:"retryCount"`
}

type serviceState struct {
	projectName string
	cache       *writeThroughCache

	// initMu serializes initialization attempts. Never take initMu while holding mu.
	initMu sync.Mutex
	// mu guards status and ready.
	mu     sync.Mutex
	status initializationStatus
	ready  bool
}

func newServiceState(projectName string) *serviceState {
	status := initializationStatus{
		State:       initStateInitializing,
		Recoverable: true,
	}
	if projectName == "" {
		status = initializationStatus{
			State:       initStateConfigError,
			Error:       errProjectRequired.Error(),
			Recoverable: false,
		}
	}
	return &serviceState{
		projectName: projectName,
		cache: &writeThroughCache{
			board: newEmptyKanbanBoard(),
			tasks: map[string]*omnifocus.Task{},
		},
		status: status,
	}
}

func (s *serviceState) initializationStatus() initializationStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *serviceState) isReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

func (s *serviceState) setStatus(status initializationStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

func (s *serviceState) setReady() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = true
	s.status = initializationStatus{
		State:       initStateReady,
		Recoverable: true,
		RetryCount:  s.status.RetryCount,
	}
}

func (s *serviceState) setDegraded(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = initializationStatus{
		State:       initStateDegraded,
		Error:       err.Error(),
		Recoverable: true,
		RetryCount:  s.status.RetryCount,
	}
}

func (s *serviceState) initializeOnce() error {
	s.initMu.Lock()
	defer s.initMu.Unlock()

	if s.projectName == "" {
		return errProjectRequired
	}

	id, err := projectIDLookup(s.projectName)
	if err != nil {
		return fmt.Errorf("project %q unavailable in OmniFocus: %w", s.projectName, err)
	}
	log.Printf("resolved project %q -> %s", s.projectName, id)

	s.cache.setProjectID(id)
	log.Printf("Loading board from OmniFocus...")
	if err := s.cache.refresh(); err != nil {
		return fmt.Errorf("initial board load failed: %w", err)
	}

	s.setReady()
	return nil
}

func (s *serviceState) refreshBoard() error {
	if !s.isReady() {
		err := s.initializeOnce()
		if err != nil && s.projectName != "" {
			s.setDegraded(err)
		}
		return err
	}
	if err := s.cache.refresh(); err != nil {
		s.setDegraded(err)
		return err
	}
	s.setReady()
	return nil
}

func (s *serviceState) retryInitialization(ctx context.Context) {
	if s.projectName == "" {
		return
	}

	delay := time.Duration(0)
	for {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}

		if s.isReady() {
			return
		}
		if err := s.initializeOnce(); err != nil {
			delay = nextRetryDelay(delay)
			nextRetry := time.Now().Add(delay)
			s.mu.Lock()
			retryCount := s.status.RetryCount + 1
			s.status = initializationStatus{
				State:       initStateRetrying,
				Error:       err.Error(),
				NextRetry:   &nextRetry,
				Recoverable: true,
				RetryCount:  retryCount,
			}
			s.mu.Unlock()
			log.Printf("initialization failed; retrying in %s: %v", delay, err)
			continue
		}
		return
	}
}

func nextRetryDelay(previous time.Duration) time.Duration {
	if previous <= 0 {
		if initialRetryDelay > maxRetryDelay {
			return maxRetryDelay
		}
		return initialRetryDelay
	}
	next := previous * 2
	if next > maxRetryDelay {
		return maxRetryDelay
	}
	return next
}

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
		return errProjectRequired
	}

	state := newServiceState(*projectName)
	go state.retryInitialization(ctx)
	go func() {
		ticker := time.NewTicker(boardRefreshInterval)
		defer ticker.Stop()
		for range ticker.C {
			if !state.isReady() {
				continue
			}
			if err := state.refreshBoard(); err != nil {
				fmt.Fprintf(os.Stderr, "board refresh error: %v", err)
			}
		}
	}()

	srv := newServer(state, *dynamicAssets)
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
