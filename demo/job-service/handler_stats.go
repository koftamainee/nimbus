package main

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (app *App) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := app.getStats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (app *App) handleTransactions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (app *App) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan SSEEvent, 64)
	app.sseClientsMu.Lock()
	app.sseClients[ch] = struct{}{}
	app.sseClientsMu.Unlock()

	defer func() {
		app.sseClientsMu.Lock()
		delete(app.sseClients, ch)
		app.sseClientsMu.Unlock()
	}()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, string(evt.Data))
			flusher.Flush()
		}
	}
}
