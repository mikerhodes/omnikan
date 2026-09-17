package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"time"
)

var (
	//go:embed assets
	assets embed.FS
)

func newServer(state *serviceState, dynamicAssets bool) http.Handler {
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

	mux.HandleFunc("GET /api/status", handleStatus(state))
	mux.HandleFunc("GET /api/board", handleBoard(state))
	mux.HandleFunc("POST /api/move", handleMove(state))
	mux.HandleFunc("POST /api/delete", handleDelete(state))
	mux.HandleFunc("POST /api/complete", handleComplete(state))
	mux.HandleFunc("POST /api/incomplete", handleIncomplete(state))
	mux.HandleFunc("POST /api/add", handleAdd(state))
	mux.HandleFunc("POST /api/edit", handleEdit(state))

	return loggingHandler(mux)
}

func handleStatus(state *serviceState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(state.initializationStatus()); err != nil {
			log.Printf("handleStatus error: %v", err)
			return
		}
	}
}

func handleBoard(state *serviceState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("force") == "true" {
			_ = state.refreshBoard()
		}
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(state.cache.getBoard())
		if err != nil {
			log.Printf("handleBoard error: %v", err)
			return
		}
	}
}

func handleMove(state *serviceState) http.HandlerFunc {
	var req struct {
		ID     string `json:"id"`
		NewCol Column `json:"newCol"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !state.isReady() {
			http.Error(w, "OmniFocus initialization is not ready", http.StatusServiceUnavailable)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		err := state.cache.moveTask(req.ID, req.NewCol)
		if err != nil {
			log.Printf("Error moving column: %v", err)
			http.Error(w, "failed to move task", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleDelete(state *serviceState) http.HandlerFunc {
	var req struct {
		ID string `json:"id"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !state.isReady() {
			http.Error(w, "OmniFocus initialization is not ready", http.StatusServiceUnavailable)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		err := state.cache.deleteTask(req.ID)
		if err != nil {
			http.Error(w, "failed to delete task", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleComplete(state *serviceState) http.HandlerFunc {
	var req struct {
		ID string `json:"id"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !state.isReady() {
			http.Error(w, "OmniFocus initialization is not ready", http.StatusServiceUnavailable)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		err := state.cache.completeTask(req.ID)
		if err != nil {
			http.Error(w, "failed to complete task", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleIncomplete(state *serviceState) http.HandlerFunc {
	var req struct {
		ID string `json:"id"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !state.isReady() {
			http.Error(w, "OmniFocus initialization is not ready", http.StatusServiceUnavailable)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		err := state.cache.uncompleteTask(req.ID)
		if err != nil {
			http.Error(w, "failed to incomplete task", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAdd(state *serviceState) http.HandlerFunc {
	var req struct {
		Name string `json:"name"`
		Col  Column `json:"col"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !state.isReady() {
			http.Error(w, "OmniFocus initialization is not ready", http.StatusServiceUnavailable)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		task, err := state.cache.addTask(req.Name, req.Col)
		if err != nil {
			log.Printf("AddTask error: %v", err)
			http.Error(w, "failed to add task", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(task)
		if err != nil {
			log.Printf("handleAdd error: %v", err)
			return
		}
	}
}

func handleEdit(state *serviceState) http.HandlerFunc {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Note string `json:"note"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !state.isReady() {
			http.Error(w, "OmniFocus initialization is not ready", http.StatusServiceUnavailable)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		task, err := state.cache.editTask(req.ID, req.Name, req.Note)
		if err != nil {
			log.Printf("EditTask error: %v", err)
			http.Error(w, "failed to edit task", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(task)
		if err != nil {
			log.Printf("handleEdit error: %v", err)
			return
		}
	}
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
