package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/mikerhodes/omnikan/internal/omnifocus"
)

var (
	//go:embed assets
	assets embed.FS
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

	srv := newServer(id, cache, *dynamicAssets)
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

//
// Main data store is a write-through cache to OmniFocus
//

type kanbanBoard struct {
	Backlog    []omnifocus.Task `json:"backlog"`
	Ready      []omnifocus.Task `json:"ready"`
	InProgress []omnifocus.Task `json:"inprogress"`
}

type cachedTask struct {
	task omnifocus.Task
	col  string
}

// writeThroughCache holds the in-memory board state and synchronises all
// mutations: every write calls OmniFocus first, then updates board and tasks
// on success, so the cache is never ahead of OmniFocus.
type writeThroughCache struct {
	projectID string

	// cacheMu protects board and tasks.
	cacheMu sync.Mutex
	board   *kanbanBoard
	tasks   map[string]*cachedTask // task ID -> task + current column
}

// getBoard returns the current cached board snapshot.
func (c *writeThroughCache) getBoard() *kanbanBoard {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	return c.board
}

// refresh fetches all columns from OmniFocus and updates the cache.
func (c *writeThroughCache) refresh() error {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	newBoard := kanbanBoard{
		Backlog:    []omnifocus.Task{},
		Ready:      []omnifocus.Task{},
		InProgress: []omnifocus.Task{},
	}
	newTasks := map[string]*cachedTask{}

	tasks := []omnifocus.Task{}
	for _, tag := range []string{
		omnifocus.TagBacklog,
		omnifocus.TagInProgress,
		omnifocus.TagReady,
	} {
		ts, err := omnifocus.TasksForTag(tag, c.projectID)
		if err != nil {
			return err
		}
		tasks = append(tasks, ts...)
	}
	for _, t := range tasks {
		col := columnForTask(&t)
		c.tasks[t.ID] = &cachedTask{task: t, col: col}
		switch col {
		case omnifocus.TagBacklog:
			newBoard.Backlog = append(newBoard.Backlog, t)
		case omnifocus.TagReady:
			newBoard.Ready = append(newBoard.Ready, t)
		case omnifocus.TagInProgress:
			newBoard.InProgress = append(newBoard.InProgress, t)
		default:
			panic("Task with unknown tag; should never happen")
		}
	}

	// Update cache on all successful
	c.board = &newBoard
	c.tasks = newTasks

	log.Printf("board cache refreshed: %d backlog, %d ready, %d inprogress",
		len(c.board.Backlog), len(c.board.Ready), len(c.board.InProgress))
	return nil
}

// moveTask swaps the kanban tag on the task in OmniFocus and moves it to the
// target column in the cache. No-ops if the task is already in newCol.
func (c *writeThroughCache) moveTask(id string, newCol string) error {
	if !isValidColumn(newCol) {
		return fmt.Errorf("invalid column name %s", newCol)
	}

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	ct, ok := c.tasks[id]
	if !ok {
		return fmt.Errorf("Invalid ID %s", id)
	}

	if ct.col == newCol {
		return nil
	}

	err := omnifocus.SwapTag(id, ct.col, newCol)
	if err != nil {
		return fmt.Errorf("swapping tag failed: %w", err)
	}

	c.board = moveBoardTask(c.board, ct.task, ct.col, newCol)

	return nil
}

// deleteTask deletes the task from OmniFocus and removes it from the cache.
func (c *writeThroughCache) deleteTask(id string) error {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	err := omnifocus.DeleteTask(id)
	if err != nil {
		return err
	}
	if ct, ok := c.tasks[id]; ok {
		c.board = removeBoardTask(c.board, id, ct.col)
		delete(c.tasks, id)
	}
	return nil
}

// completeTask marks the task complete in OmniFocus and removes it from the cache.
func (c *writeThroughCache) completeTask(id string) error {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	err := omnifocus.MarkComplete(id)
	if err != nil {
		return err
	}
	if ct, ok := c.tasks[id]; ok {
		c.board = removeBoardTask(c.board, id, ct.col)
		delete(c.tasks, id)
	}
	return nil
}

// uncompleteTask marks the task incomplete in OmniFocus
// and restores it to the cache.
func (c *writeThroughCache) uncompleteTask(id string) error {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	err := omnifocus.MarkIncomplete(id)
	if err != nil {
		return err
	}
	t, err := omnifocus.GetTask(id)
	if err != nil {
		return err
	}
	col := columnForTask(&t)
	ct := &cachedTask{task: t, col: col}
	c.tasks[t.ID] = ct
	c.board = addBoardTask(c.board, ct.task, ct.col)
	return nil
}

// addTask creates the task in OmniFocus and inserts it into the cache.
func (c *writeThroughCache) addTask(name string, col string, projectID string) (*cachedTask, error) {
	if name == "" || !isValidColumn(col) {
		return nil, fmt.Errorf("invalid column %s", col)
	}

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	task, err := omnifocus.AddTask(name, col, projectID)
	if err != nil {
		return nil, err
	}
	ct := &cachedTask{task: task, col: col}
	c.tasks[ct.task.ID] = ct
	c.board = addBoardTask(c.board, task, col)
	return ct, nil
}

//
// HTTP server
//

func newServer(projectID string, cache *writeThroughCache, dynamicAssets bool) http.Handler {
	mux := http.NewServeMux()

	// Serve assets either from disk (useful for debug) or
	// from embed in binary.
	if dynamicAssets {
		mux.Handle("GET /", http.FileServer(http.Dir("assets")))
	} else {
		assetsSub, err := fs.Sub(assets, "assets")
		if err != nil {
			panic("Could not load assets from binary")
		}
		mux.Handle("GET /", http.FileServerFS(assetsSub))
	}

	mux.HandleFunc("GET /api/board", handleBoard(cache))
	mux.HandleFunc("POST /api/move", handleMove(cache))
	mux.HandleFunc("POST /api/delete", handleDelete(cache))
	mux.HandleFunc("POST /api/complete", handleComplete(cache))
	mux.HandleFunc("POST /api/incomplete", handleIncomplete(cache))
	mux.HandleFunc("POST /api/add", handleAdd(cache, projectID))

	return loggingHandler(mux)
}

func handleBoard(cache *writeThroughCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("force") == "true" {
			if err := cache.refresh(); err != nil {
				http.Error(w, "failed to refresh board", http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cache.getBoard()) //nolint:errcheck
	}
}

func handleMove(cache *writeThroughCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     string `json:"id"`
			NewCol string `json:"newCol"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		err := cache.moveTask(req.ID, req.NewCol)
		if err != nil {
			log.Printf("Error moving column: %v", err)
			http.Error(w, "failed to move task", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleDelete(cache *writeThroughCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		err := cache.deleteTask(req.ID)
		if err != nil {
			http.Error(w, "failed to delete task", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleComplete(cache *writeThroughCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		err := cache.completeTask(req.ID)
		if err != nil {
			http.Error(w, "failed to complete task", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleIncomplete(cache *writeThroughCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		err := cache.uncompleteTask(req.ID)
		if err != nil {
			http.Error(w, "failed to incomplete task", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAdd(cache *writeThroughCache, projectID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
			Col  string `json:"col"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		task, err := cache.addTask(req.Name, req.Col, projectID)
		if err != nil {
			log.Printf("AddTask error: %v", err)
			http.Error(w, "failed to add task", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(task) //nolint:errcheck
	}
}

// columnForTask returns the kanban column for a task by inspecting its tags.
// Returns the first tag that matches a kanban column constant, or empty string.
func columnForTask(t *omnifocus.Task) string {
	for _, tag := range t.Tags {
		if tag == omnifocus.TagBacklog || tag == omnifocus.TagReady || tag == omnifocus.TagInProgress {
			return tag
		}
	}
	return ""
}

// isValidColumn returns true for the valid kanban columns.
func isValidColumn(col string) bool {
	return col == omnifocus.TagBacklog ||
		col == omnifocus.TagReady ||
		col == omnifocus.TagInProgress
}

// colSlice returns a pointer to the board slice for the given column.
func colSlice(b *kanbanBoard, col string) *[]omnifocus.Task {
	switch col {
	case omnifocus.TagBacklog:
		return &b.Backlog
	case omnifocus.TagReady:
		return &b.Ready
	case omnifocus.TagInProgress:
		return &b.InProgress
	}
	return nil
}

// moveBoardTask moves a task from one column slice to another in the board.
func moveBoardTask(b *kanbanBoard, t omnifocus.Task, fromCol, toCol string) *kanbanBoard {
	b = removeBoardTask(b, t.ID, fromCol)
	b = addBoardTask(b, t, toCol)
	return b
}

// removeBoardTask removes a task by ID from its column slice.
func removeBoardTask(b *kanbanBoard, id, col string) *kanbanBoard {
	s := colSlice(b, col)
	if s == nil {
		return b
	}
	filtered := (*s)[:0]
	for _, t := range *s {
		if t.ID != id {
			filtered = append(filtered, t)
		}
	}
	*s = filtered
	return b
}

// addBoardTask appends a task to a column slice.
func addBoardTask(b *kanbanBoard, t omnifocus.Task, col string) *kanbanBoard {
	s := colSlice(b, col)
	if s == nil {
		return b
	}
	*s = append(*s, t)
	return b
}

//
// Middlewares
//

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func loggingHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{w, http.StatusOK}
		h.ServeHTTP(rw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.statusCode, time.Since(start))
	})
}
