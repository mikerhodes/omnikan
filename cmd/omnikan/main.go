package main

import (
	"embed"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/mikerhodes/omnikan/internal/omnifocus"
)

var (
	//go:embed static
	static embed.FS
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

// cache holds the last fetched board and a per-task lookup (including
// completed tasks within the 60s undo window) for moves and completions.
var cache struct {
	mu    sync.Mutex
	board boardResponse
	tasks map[string]cachedTask // task ID -> task + current column
}

// moveMu serialises calls to OmniFocus so concurrent move requests are
// queued rather than run in parallel against the single-threaded JXA bridge.
var moveMu sync.Mutex

func main() {
	log.Printf("Loading board from OmniFocus...")
	if err := refreshCache(); err != nil {
		log.Fatalf("initial board load failed: %v", err)
	}
	go func() {
		for range time.Tick(boardRefreshInterval) {
			if err := refreshCache(); err != nil {
				log.Printf("board refresh error: %v", err)
			}
		}
	}()

	mux := http.NewServeMux()

	// Serve board.html at /
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		data, err := static.ReadFile("static/board.html") //nolint:goembedcheck
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusNotFound, time.Since(start))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data) //nolint:errcheck
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusOK, time.Since(start))
	})

	// Return the cached board as JSON
	mux.HandleFunc("GET /api/board", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		cache.mu.Lock()
		board := cache.board
		cache.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(board) //nolint:errcheck
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusOK, time.Since(start))
	})

	// Move a task to a new column by swapping its kanban tag
	mux.HandleFunc("POST /api/move", func(w http.ResponseWriter, r *http.Request) {
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
		moveMu.Lock()
		cache.mu.Lock()
		ct, ok := cache.tasks[req.ID]
		cache.mu.Unlock()

		if !ok {
			moveMu.Unlock()
			http.Error(w, "task not found in cache", http.StatusNotFound)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusNotFound, time.Since(start))
			return
		}
		if ct.col == req.NewCol {
			moveMu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusNoContent, time.Since(start))
			return
		}

		err := omnifocus.SwapTag(req.ID, ct.col, req.NewCol)
		if err == nil {
			cache.mu.Lock()
			cache.tasks[req.ID] = cachedTask{task: ct.task, col: req.NewCol}
			cache.board = moveBoardTask(cache.board, ct.task, ct.col, req.NewCol)
			cache.mu.Unlock()
		}
		moveMu.Unlock()

		if err != nil {
			log.Printf("SwapTag error: %v", err)
			http.Error(w, "failed to move task", http.StatusInternalServerError)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusInternalServerError, time.Since(start))
			return
		}

		w.WriteHeader(http.StatusNoContent)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusNoContent, time.Since(start))
	})

	// Mark a task complete in OmniFocus
	mux.HandleFunc("POST /api/complete", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusBadRequest, time.Since(start))
			return
		}
		moveMu.Lock()
		err := omnifocus.MarkComplete(req.ID)
		if err == nil {
			cache.mu.Lock()
			if ct, ok := cache.tasks[req.ID]; ok {
				cache.board = removeBoardTask(cache.board, req.ID, ct.col)
			}
			cache.mu.Unlock()
		}
		moveMu.Unlock()
		if err != nil {
			http.Error(w, "failed to complete task", http.StatusInternalServerError)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusInternalServerError, time.Since(start))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusNoContent, time.Since(start))
	})

	// Mark a task incomplete in OmniFocus (undo complete)
	mux.HandleFunc("POST /api/incomplete", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusBadRequest, time.Since(start))
			return
		}
		moveMu.Lock()
		err := omnifocus.MarkIncomplete(req.ID)
		if err == nil {
			cache.mu.Lock()
			if ct, ok := cache.tasks[req.ID]; ok {
				cache.board = addBoardTask(cache.board, ct.task, ct.col)
			}
			cache.mu.Unlock()
		}
		moveMu.Unlock()
		if err != nil {
			http.Error(w, "failed to incomplete task", http.StatusInternalServerError)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusInternalServerError, time.Since(start))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusNoContent, time.Since(start))
	})

	addr := ":8080"
	log.Printf("Listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

// refreshCache fetches all columns from OmniFocus and updates the cache.
func refreshCache() error {
	board, tasks, err := fetchBoard()
	if err != nil {
		return err
	}
	cache.mu.Lock()
	cache.board = board
	cache.tasks = tasks
	cache.mu.Unlock()
	log.Printf("board cache refreshed: %d backlog, %d ready, %d inprogress",
		len(board.Backlog), len(board.Ready), len(board.InProgress))
	return nil
}

// isMovableColumn returns true for the valid kanban columns.
func isMovableColumn(col string) bool {
	return col == omnifocus.TagBacklog ||
		col == omnifocus.TagReady ||
		col == omnifocus.TagInProgress
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
func fetchBoard() (boardResponse, map[string]cachedTask, error) {
	var board boardResponse
	tasks := map[string]cachedTask{}

	backlog, err := omnifocus.TasksForTag(omnifocus.TagBacklog)
	if err != nil {
		return board, nil, err
	}
	board.Backlog = backlog
	for _, t := range backlog {
		tasks[t.ID] = cachedTask{task: t, col: omnifocus.TagBacklog}
	}

	ready, err := omnifocus.TasksForTag(omnifocus.TagReady)
	if err != nil {
		return board, nil, err
	}
	board.Ready = ready
	for _, t := range ready {
		tasks[t.ID] = cachedTask{task: t, col: omnifocus.TagReady}
	}

	inprogress, err := omnifocus.TasksForTag(omnifocus.TagInProgress)
	if err != nil {
		return board, nil, err
	}
	board.InProgress = inprogress
	for _, t := range inprogress {
		tasks[t.ID] = cachedTask{task: t, col: omnifocus.TagInProgress}
	}

	return board, tasks, nil
}
