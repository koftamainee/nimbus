package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
)

type TxEvent struct {
	ID            string `json:"id"`
	DebitAccount  string `json:"debit_account"`
	CreditAccount string `json:"credit_account"`
	Amount        uint64 `json:"amount"`
	Currency      string `json:"currency"`
}

type TxResult struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	ErrorMsg  string `json:"error_msg,omitempty"`
	LatencyMs int64  `json:"latency_ms"`
	WorkerID  string `json:"worker_id,omitempty"`
}

type Stats struct {
	TotalSent      int64   `json:"total_sent"`
	TotalConfirmed int64   `json:"total_confirmed"`
	TotalErrors    int64   `json:"total_errors"`
	TPS            float64 `json:"tps"`
	ActiveWorkers  int     `json:"active_workers"`
	StartTime      string  `json:"started_at"`
	UptimeSecs     int64   `json:"uptime_secs"`
}

type SSEEvent struct {
	Type string
	Data []byte
}

type App struct {
	nc        *nats.Conn
	mu        sync.Mutex

	totalSent      atomic.Int64
	totalConfirmed atomic.Int64
	totalErrors    atomic.Int64

	tps   float64
	tpsMu sync.RWMutex

	startedAt time.Time

	sseClients   map[chan SSEEvent]struct{}
	sseClientsMu sync.Mutex

	generatorStop chan struct{}
}

func main() {
	natsAddr := os.Getenv("NATS_ADDR")
	if natsAddr == "" {
		natsAddr = "localhost:4222"
	}
	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":9090"
	}

	nc, err := nats.Connect(natsAddr,
		nats.Name("job-service"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		log.Fatalf("nats connect: %v", err)
	}
	defer nc.Close()
	log.Printf("connected to NATS at %s", natsAddr)

	app := &App{
		nc:         nc,
		startedAt:  time.Now(),
		sseClients: make(map[chan SSEEvent]struct{}),
	}

	nc.Subscribe("tx.completed", func(msg *nats.Msg) {
		var result TxResult
		if err := json.Unmarshal(msg.Data, &result); err != nil {
			return
		}
		if result.Status == "confirmed" {
			app.totalConfirmed.Add(1)
		} else {
			app.totalErrors.Add(1)
		}
	})

	go app.tpsCalculator()
	go app.eventBroadcaster()

	mux := http.NewServeMux()
	mux.HandleFunc("/start", app.corsWrap(app.handleStart))
	mux.HandleFunc("/stop", app.corsWrap(app.handleStop))
	mux.HandleFunc("/stats", app.corsWrap(app.handleStats))
	mux.HandleFunc("/api/transactions", app.corsWrap(app.handleTransactions))
	mux.HandleFunc("/api/events", app.corsWrap(app.handleSSE))

	log.Printf("job-service listening on %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}

func (app *App) corsWrap(fn func(w http.ResponseWriter, r *http.Request)) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		fn(w, r)
	}
}

func (app *App) tpsCalculator() {
	var prev int64
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		curr := app.totalConfirmed.Load()
		app.tpsMu.Lock()
		app.tps = float64(curr-prev) / 1.0
		app.tpsMu.Unlock()
		prev = curr
	}
}

func (app *App) eventBroadcaster() {
	ticker := time.NewTicker(500 * time.Millisecond)
	for range ticker.C {
		stats := app.getStats()
		data, _ := json.Marshal(stats)
		app.broadcastSSE("stats", data)
	}
}

func (app *App) getStats() Stats {
	app.tpsMu.RLock()
	tps := app.tps
	app.tpsMu.RUnlock()

	return Stats{
		TotalSent:      app.totalSent.Load(),
		TotalConfirmed: app.totalConfirmed.Load(),
		TotalErrors:    app.totalErrors.Load(),
		TPS:            tps,
		StartTime:      app.startedAt.Format(time.RFC3339),
		UptimeSecs:     int64(time.Since(app.startedAt).Seconds()),
	}
}

func (app *App) broadcastSSE(eventType string, data []byte) {
	evt := SSEEvent{Type: eventType, Data: data}
	app.sseClientsMu.Lock()
	defer app.sseClientsMu.Unlock()
	for ch := range app.sseClients {
		select {
		case ch <- evt:
		default:
		}
	}
}
