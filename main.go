package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/mikerhodes/omnikan/internal/omnifocus"
)

var (
	//go:embed assets
	assets embed.FS
)

const boardRefreshInterval = 10 * time.Minute

type boardResponse struct {
	Backlog    []omnifocus.Task `json:"backlog"`
	Ready      []omnifocus.Task `json:"ready"`
	InProgress []omnifocus.Task `json:"inprogress"`
}

type cachedTask struct {
	task omnifocus.Task
	col  string
}

type server struct {
	projectID string

	// moveMu serialises calls to OmniFocus so concurrent move requests are
	// queued rather than run in parallel against the single-threaded JXA bridge.
	moveMu sync.Mutex

	// cacheMu protects board and tasks.
	cacheMu sync.Mutex
	board   boardResponse
	tasks   map[string]cachedTask // task ID -> task + current column

	mux *http.ServeMux
}

func newServer(projectID string) *server {
	s := &server{projectID: projectID}
	s.mux = http.NewServeMux()

	s.mux.HandleFunc("GET /", s.handleIndex())
	s.mux.Handle("GET /assets/", http.FileServerFS(assets))
	s.mux.HandleFunc("GET /api/board", s.handleBoard())
	s.mux.HandleFunc("POST /api/move", s.handleMove())
	s.mux.HandleFunc("POST /api/delete", s.handleDelete())
	s.mux.HandleFunc("POST /api/complete", s.handleComplete())
	s.mux.HandleFunc("POST /api/incomplete", s.handleIncomplete())
	s.mux.HandleFunc("POST /api/add", s.handleAdd())

	return s
}

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("omnikan", flag.ContinueOnError)
	projectName := flags.String("project", omnifocus.ProjectName, "OmniFocus project name")
	addr := flags.String("addr", "localhost:8080", "listen address")
	if err := flags.Parse(args); err != nil {
		return err
	}

	id, err := omnifocus.ProjectID(*projectName)
	if err != nil {
		return fmt.Errorf("project %q not found in OmniFocus: %w", *projectName, err)
	}
	log.Printf("resolved project %q -> %s", *projectName, id)

	srv := newServer(id)

	log.Printf("Loading board from OmniFocus...")
	if err := srv.refreshCache(); err != nil {
		return fmt.Errorf("initial board load failed: %w", err)
	}
	go func() {
		for range time.Tick(boardRefreshInterval) {
			if err := srv.refreshCache(); err != nil {
				log.Printf("board refresh error: %v", err)
			}
		}
	}()

	host, port, _ := net.SplitHostPort(*addr)
	if host == "" {
		host = "localhost"
	}
	log.Printf("Listening on http://%s", net.JoinHostPort(host, port))
	return http.ListenAndServe(*addr, srv)
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *server) handleIndex() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		data, err := assets.ReadFile("assets/board.html") //nolint:goembedcheck
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusNotFound, time.Since(start))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data) //nolint:errcheck
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusOK, time.Since(start))
	}
}

func (s *server) handleBoard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		if r.URL.Query().Get("force") == "true" {
			if err := s.refreshCache(); err != nil {
				http.Error(w, "failed to refresh board", http.StatusInternalServerError)
				log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusInternalServerError, time.Since(start))
				return
			}
		}

		s.cacheMu.Lock()
		board := s.board
		s.cacheMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(board) //nolint:errcheck
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusOK, time.Since(start))
	}
}

func (s *server) handleMove() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		var req struct {
			ID     string `json:"id"`
			NewCol string `json:"newCol"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusBadRequest, time.Since(start))
			return
		}

		if !isMovableColumn(req.NewCol) {
			http.Error(w, "invalid column", http.StatusBadRequest)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusBadRequest, time.Since(start))
			return
		}

		// moveMu is held for the cache read, JXA call, and cache write together
		// so that rapid moves of the same card always use the latest oldCol.
		s.moveMu.Lock()
		s.cacheMu.Lock()
		ct, ok := s.tasks[req.ID]
		s.cacheMu.Unlock()

		if !ok {
			s.moveMu.Unlock()
			http.Error(w, "task not found in cache", http.StatusNotFound)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusNotFound, time.Since(start))
			return
		}
		if ct.col == req.NewCol {
			s.moveMu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusNoContent, time.Since(start))
			return
		}

		err := omnifocus.SwapTag(req.ID, ct.col, req.NewCol)
		if err == nil {
			s.cacheMu.Lock()
			s.tasks[req.ID] = cachedTask{task: ct.task, col: req.NewCol}
			s.board = moveBoardTask(s.board, ct.task, ct.col, req.NewCol)
			s.cacheMu.Unlock()
		}
		s.moveMu.Unlock()

		if err != nil {
			log.Printf("SwapTag error: %v", err)
			http.Error(w, "failed to move task", http.StatusInternalServerError)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusInternalServerError, time.Since(start))
			return
		}

		w.WriteHeader(http.StatusNoContent)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusNoContent, time.Since(start))
	}
}

func (s *server) handleDelete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusBadRequest, time.Since(start))
			return
		}
		s.moveMu.Lock()
		err := omnifocus.DeleteTask(req.ID)
		if err == nil {
			s.cacheMu.Lock()
			if ct, ok := s.tasks[req.ID]; ok {
				s.board = removeBoardTask(s.board, req.ID, ct.col)
			}
			s.cacheMu.Unlock()
		}
		s.moveMu.Unlock()
		if err != nil {
			http.Error(w, "failed to delete task", http.StatusInternalServerError)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusInternalServerError, time.Since(start))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusNoContent, time.Since(start))
	}
}

func (s *server) handleComplete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusBadRequest, time.Since(start))
			return
		}
		s.moveMu.Lock()
		err := omnifocus.MarkComplete(req.ID)
		if err == nil {
			s.cacheMu.Lock()
			if ct, ok := s.tasks[req.ID]; ok {
				s.board = removeBoardTask(s.board, req.ID, ct.col)
			}
			s.cacheMu.Unlock()
		}
		s.moveMu.Unlock()
		if err != nil {
			http.Error(w, "failed to complete task", http.StatusInternalServerError)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusInternalServerError, time.Since(start))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusNoContent, time.Since(start))
	}
}

func (s *server) handleIncomplete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusBadRequest, time.Since(start))
			return
		}
		s.moveMu.Lock()
		err := omnifocus.MarkIncomplete(req.ID)
		if err == nil {
			s.cacheMu.Lock()
			if ct, ok := s.tasks[req.ID]; ok {
				s.board = addBoardTask(s.board, ct.task, ct.col)
			}
			s.cacheMu.Unlock()
		}
		s.moveMu.Unlock()
		if err != nil {
			http.Error(w, "failed to incomplete task", http.StatusInternalServerError)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusInternalServerError, time.Since(start))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusNoContent, time.Since(start))
	}
}

func (s *server) handleAdd() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var req struct {
			Name string `json:"name"`
			Col  string `json:"col"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusBadRequest, time.Since(start))
			return
		}
		if req.Name == "" || !isMovableColumn(req.Col) {
			http.Error(w, "invalid request", http.StatusBadRequest)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusBadRequest, time.Since(start))
			return
		}

		s.moveMu.Lock()
		task, err := omnifocus.AddTask(req.Name, req.Col, s.projectID)
		if err == nil {
			s.cacheMu.Lock()
			s.tasks[task.ID] = cachedTask{task: task, col: req.Col}
			s.board = addBoardTask(s.board, task, req.Col)
			s.cacheMu.Unlock()
		}
		s.moveMu.Unlock()

		if err != nil {
			log.Printf("AddTask error: %v", err)
			http.Error(w, "failed to add task", http.StatusInternalServerError)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusInternalServerError, time.Since(start))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(task) //nolint:errcheck
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusOK, time.Since(start))
	}
}

// isMovableColumn returns true for the valid kanban columns.
func isMovableColumn(col string) bool {
	return col == omnifocus.TagBacklog ||
		col == omnifocus.TagReady ||
		col == omnifocus.TagInProgress
}

// refreshCache fetches all columns from OmniFocus and updates the cache.
func (s *server) refreshCache() error {
	board, tasks, err := s.fetchBoard()
	if err != nil {
		return err
	}
	s.cacheMu.Lock()
	s.board = board
	s.tasks = tasks
	s.cacheMu.Unlock()
	log.Printf("board cache refreshed: %d backlog, %d ready, %d inprogress",
		len(board.Backlog), len(board.Ready), len(board.InProgress))
	return nil
}

// colSlice returns a pointer to the board slice for the given column.
func colSlice(b *boardResponse, col string) *[]omnifocus.Task {
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
func moveBoardTask(b boardResponse, t omnifocus.Task, fromCol, toCol string) boardResponse {
	b = removeBoardTask(b, t.ID, fromCol)
	b = addBoardTask(b, t, toCol)
	return b
}

// removeBoardTask removes a task by ID from its column slice.
func removeBoardTask(b boardResponse, id, col string) boardResponse {
	s := colSlice(&b, col)
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
func addBoardTask(b boardResponse, t omnifocus.Task, col string) boardResponse {
	s := colSlice(&b, col)
	if s == nil {
		return b
	}
	*s = append(*s, t)
	return b
}

// fetchBoard retrieves tasks for all columns from OmniFocus.
// Calls are sequential because the OmniFocus scripting bridge is not re-entrant.
func (s *server) fetchBoard() (boardResponse, map[string]cachedTask, error) {
	var board boardResponse
	tasks := map[string]cachedTask{}

	backlog, err := omnifocus.TasksForTag(omnifocus.TagBacklog, s.projectID)
	if err != nil {
		return board, nil, err
	}
	board.Backlog = backlog
	for _, t := range backlog {
		tasks[t.ID] = cachedTask{task: t, col: omnifocus.TagBacklog}
	}

	ready, err := omnifocus.TasksForTag(omnifocus.TagReady, s.projectID)
	if err != nil {
		return board, nil, err
	}
	board.Ready = ready
	for _, t := range ready {
		tasks[t.ID] = cachedTask{task: t, col: omnifocus.TagReady}
	}

	inprogress, err := omnifocus.TasksForTag(omnifocus.TagInProgress, s.projectID)
	if err != nil {
		return board, nil, err
	}
	board.InProgress = inprogress
	for _, t := range inprogress {
		tasks[t.ID] = cachedTask{task: t, col: omnifocus.TagInProgress}
	}

	return board, tasks, nil
}
