package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mikerhodes/omnikan/internal/omnifocus"
)

func TestNewServiceStateConfigError(t *testing.T) {
	state := newServiceState("")
	status := state.initializationStatus()

	if status.State != initStateConfigError {
		t.Fatalf("state = %q, want %q", status.State, initStateConfigError)
	}
	if status.Recoverable {
		t.Fatal("configuration error should not be recoverable")
	}
	if status.Error == "" {
		t.Fatal("configuration error should include an error message")
	}
}

func TestRunRequiresProject(t *testing.T) {
	err := run(context.Background(), []string{"-addr", "127.0.0.1:0"})
	if !errors.Is(err, errProjectRequired) {
		t.Fatalf("run() error = %v, want %v", err, errProjectRequired)
	}
}

func TestRefreshBoardPreservesConfigError(t *testing.T) {
	state := newServiceState("")

	err := state.refreshBoard()
	if !errors.Is(err, errProjectRequired) {
		t.Fatalf("refreshBoard() error = %v, want %v", err, errProjectRequired)
	}

	status := state.initializationStatus()
	if status.State != initStateConfigError {
		t.Fatalf("state = %q, want %q", status.State, initStateConfigError)
	}
	if status.Recoverable {
		t.Fatal("configuration error should not become recoverable")
	}
}

func TestInitializeOnceReady(t *testing.T) {
	restore := stubOmniFocus(
		func(projectName string) (string, error) {
			if projectName != "Kanban" {
				t.Fatalf("project name = %q, want Kanban", projectName)
			}
			return "project-1", nil
		},
		func(projectID string) ([]omnifocus.Task, error) {
			if projectID != "project-1" {
				t.Fatalf("project ID = %q, want project-1", projectID)
			}
			return []omnifocus.Task{{
				ID:   "task-1",
				Name: "Task 1",
				Tags: []string{omnifocus.TagReady},
			}}, nil
		},
	)
	defer restore()

	state := newServiceState("Kanban")
	if err := state.initializeOnce(); err != nil {
		t.Fatalf("initializeOnce() error = %v", err)
	}

	status := state.initializationStatus()
	if status.State != initStateReady {
		t.Fatalf("state = %q, want %q", status.State, initStateReady)
	}
	if !state.isReady() {
		t.Fatal("state should be ready")
	}
	board := state.cache.getBoard()
	if len(board.Ready) != 1 || board.Ready[0].ID != "task-1" {
		t.Fatalf("ready board = %#v, want task-1", board.Ready)
	}
}

func TestRetryInitializationTransitionsToReady(t *testing.T) {
	oldInitialRetryDelay := initialRetryDelay
	oldMaxRetryDelay := maxRetryDelay
	initialRetryDelay = time.Millisecond
	maxRetryDelay = time.Millisecond
	defer func() {
		initialRetryDelay = oldInitialRetryDelay
		maxRetryDelay = oldMaxRetryDelay
	}()

	attempts := 0
	restore := stubOmniFocus(
		func(projectName string) (string, error) {
			attempts++
			if attempts == 1 {
				return "", errors.New("OmniFocus unavailable")
			}
			return "project-1", nil
		},
		func(projectID string) ([]omnifocus.Task, error) {
			return []omnifocus.Task{{
				ID:   "task-1",
				Name: "Task 1",
				Tags: []string{omnifocus.TagBacklog},
			}}, nil
		},
	)
	defer restore()

	state := newServiceState("Kanban")
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer close(done)
		state.retryInitialization(ctx)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retryInitialization did not complete")
	}

	status := state.initializationStatus()
	if status.State != initStateReady {
		t.Fatalf("state = %q, want %q", status.State, initStateReady)
	}
	if status.RetryCount != 1 {
		t.Fatalf("retry count = %d, want 1", status.RetryCount)
	}
	if !state.isReady() {
		t.Fatal("state should be ready after retry")
	}
}

func TestNextRetryDelayIsBounded(t *testing.T) {
	oldInitialRetryDelay := initialRetryDelay
	oldMaxRetryDelay := maxRetryDelay
	initialRetryDelay = time.Second
	maxRetryDelay = 3 * time.Second
	defer func() {
		initialRetryDelay = oldInitialRetryDelay
		maxRetryDelay = oldMaxRetryDelay
	}()

	tests := []struct {
		previous time.Duration
		want     time.Duration
	}{
		{0, time.Second},
		{time.Second, 2 * time.Second},
		{2 * time.Second, 3 * time.Second},
		{3 * time.Second, 3 * time.Second},
	}

	for _, tt := range tests {
		if got := nextRetryDelay(tt.previous); got != tt.want {
			t.Fatalf("nextRetryDelay(%s) = %s, want %s", tt.previous, got, tt.want)
		}
	}
}

func TestInitialRetryDelayIsBounded(t *testing.T) {
	oldInitialRetryDelay := initialRetryDelay
	oldMaxRetryDelay := maxRetryDelay
	initialRetryDelay = 5 * time.Second
	maxRetryDelay = time.Second
	defer func() {
		initialRetryDelay = oldInitialRetryDelay
		maxRetryDelay = oldMaxRetryDelay
	}()

	if got := nextRetryDelay(0); got != time.Second {
		t.Fatalf("nextRetryDelay(0) = %s, want %s", got, time.Second)
	}
}

func TestServerReachableBeforeInitializationReady(t *testing.T) {
	state := newServiceState("Kanban")
	srv := newServer(state, false)

	statusReq := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	statusRec := httptest.NewRecorder()
	srv.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("GET /api/status status = %d, want %d", statusRec.Code, http.StatusOK)
	}
	var status initializationStatus
	if err := json.NewDecoder(statusRec.Body).Decode(&status); err != nil {
		t.Fatalf("decoding status: %v", err)
	}
	if status.State != initStateInitializing {
		t.Fatalf("state = %q, want %q", status.State, initStateInitializing)
	}

	boardReq := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	boardRec := httptest.NewRecorder()
	srv.ServeHTTP(boardRec, boardReq)
	if boardRec.Code != http.StatusOK {
		t.Fatalf("GET /api/board status = %d, want %d", boardRec.Code, http.StatusOK)
	}
	var board kanbanBoard
	if err := json.NewDecoder(boardRec.Body).Decode(&board); err != nil {
		t.Fatalf("decoding board: %v", err)
	}
	if len(board.Backlog) != 0 || len(board.Ready) != 0 || len(board.InProgress) != 0 {
		t.Fatalf("board = %#v, want empty board", board)
	}

	addReq := httptest.NewRequest(http.MethodPost, "/api/add", strings.NewReader(`{"name":"Task","col":"backlog"}`))
	addReq.Header.Set("Content-Type", "application/json")
	addRec := httptest.NewRecorder()
	srv.ServeHTTP(addRec, addReq)
	if addRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /api/add status = %d, want %d", addRec.Code, http.StatusServiceUnavailable)
	}
}

func stubOmniFocus(
	projectID func(string) (string, error),
	tasks func(string) ([]omnifocus.Task, error),
) func() {
	oldProjectIDLookup := projectIDLookup
	oldTasksForProject := tasksForProject
	projectIDLookup = projectID
	tasksForProject = tasks
	return func() {
		projectIDLookup = oldProjectIDLookup
		tasksForProject = oldTasksForProject
	}
}
