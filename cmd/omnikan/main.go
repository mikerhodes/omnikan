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

type boardResponse struct {
	Backlog    []omnifocus.Task `json:"backlog"`
	Ready      []omnifocus.Task `json:"ready"`
	InProgress []omnifocus.Task `json:"inprogress"`
}

// cache holds the last fetched board and a map of task ID -> column tag,
// used to look up a task's current tag when moving it.
var cache struct {
	mu      sync.Mutex
	board   boardResponse
	taskCol map[string]string // task ID -> tag name
}

// moveMu serialises calls to OmniFocus so concurrent move requests are
// queued rather than run in parallel against the single-threaded JXA bridge.
var moveMu sync.Mutex

func main() {
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

	// Fetch board from OmniFocus, update cache, return JSON
	mux.HandleFunc("GET /api/board", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		board, taskCol, err := fetchBoard()
		if err != nil {
			log.Printf("fetchBoard error: %v", err)
			http.Error(w, "failed to fetch board", http.StatusInternalServerError)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusInternalServerError, time.Since(start))
			return
		}
		cache.mu.Lock()
		cache.board = board
		cache.taskCol = taskCol
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
		oldCol, ok := cache.taskCol[req.ID]
		cache.mu.Unlock()

		if !ok {
			moveMu.Unlock()
			http.Error(w, "task not found in cache", http.StatusNotFound)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusNotFound, time.Since(start))
			return
		}
		if oldCol == req.NewCol {
			moveMu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusNoContent, time.Since(start))
			return
		}

		err := omnifocus.SwapTag(req.ID, oldCol, req.NewCol)
		if err == nil {
			cache.mu.Lock()
			cache.taskCol[req.ID] = req.NewCol
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

// isMovableColumn returns true for the valid kanban columns.
func isMovableColumn(col string) bool {
	return col == omnifocus.TagBacklog ||
		col == omnifocus.TagReady ||
		col == omnifocus.TagInProgress
}

// fetchBoard retrieves tasks for all four Kanban columns from OmniFocus.
// Calls are sequential because the OmniFocus scripting bridge is not re-entrant.
// Returns the board and a map of task ID -> column tag for cache use.
func fetchBoard() (boardResponse, map[string]string, error) {
	var board boardResponse
	taskCol := map[string]string{}

	backlog, err := omnifocus.TasksForTag(omnifocus.TagBacklog)
	if err != nil {
		return board, nil, err
	}
	board.Backlog = backlog
	for _, t := range backlog {
		taskCol[t.ID] = omnifocus.TagBacklog
	}

	ready, err := omnifocus.TasksForTag(omnifocus.TagReady)
	if err != nil {
		return board, nil, err
	}
	board.Ready = ready
	for _, t := range ready {
		taskCol[t.ID] = omnifocus.TagReady
	}

	inprogress, err := omnifocus.TasksForTag(omnifocus.TagInProgress)
	if err != nil {
		return board, nil, err
	}
	board.InProgress = inprogress
	for _, t := range inprogress {
		taskCol[t.ID] = omnifocus.TagInProgress
	}

	return board, taskCol, nil
}
