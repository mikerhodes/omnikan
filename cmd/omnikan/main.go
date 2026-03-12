package main

import (
	"embed"
	"encoding/json"
	"log"
	"net/http"
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
	Done       []omnifocus.Task `json:"done"`
}

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

	// Serve board data as JSON
	mux.HandleFunc("GET /api/board", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		board, err := fetchBoard()
		if err != nil {
			log.Printf("fetchBoard error: %v", err)
			http.Error(w, "failed to fetch board", http.StatusInternalServerError)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusInternalServerError, time.Since(start))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(board) //nolint:errcheck
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, http.StatusOK, time.Since(start))
	})

	addr := ":8080"
	log.Printf("Listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

// fetchBoard retrieves tasks for all four Kanban columns from OmniFocus.
// Calls are sequential because the OmniFocus scripting bridge is not re-entrant.
func fetchBoard() (boardResponse, error) {
	var board boardResponse

	backlog, err := omnifocus.TasksForTag(omnifocus.TagBacklog)
	if err != nil {
		return board, err
	}
	board.Backlog = backlog

	ready, err := omnifocus.TasksForTag(omnifocus.TagReady)
	if err != nil {
		return board, err
	}
	board.Ready = ready

	inprogress, err := omnifocus.TasksForTag(omnifocus.TagInProgress)
	if err != nil {
		return board, err
	}
	board.InProgress = inprogress

	done, err := omnifocus.TasksForTag(omnifocus.TagDone)
	if err != nil {
		return board, err
	}
	board.Done = done

	return board, nil
}
