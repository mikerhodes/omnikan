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

func newServer(cache *writeThroughCache, dynamicAssets bool) http.Handler {
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
	mux.HandleFunc("POST /api/add", handleAdd(cache))
	mux.HandleFunc("POST /api/edit", handleEdit(cache))

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
		err := json.NewEncoder(w).Encode(cache.getBoard())
		if err != nil {
			log.Printf("handleBoard error: %v", err)
			return
		}
	}
}

func handleMove(cache *writeThroughCache) http.HandlerFunc {
	var req struct {
		ID     string `json:"id"`
		NewCol Column `json:"newCol"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		ID string `json:"id"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		ID string `json:"id"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		ID string `json:"id"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
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

func handleAdd(cache *writeThroughCache) http.HandlerFunc {
	var req struct {
		Name string `json:"name"`
		Col  Column `json:"col"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		task, err := cache.addTask(req.Name, req.Col)
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

func handleEdit(cache *writeThroughCache) http.HandlerFunc {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Note string `json:"note"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		task, err := cache.editTask(req.ID, req.Name, req.Note)
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
