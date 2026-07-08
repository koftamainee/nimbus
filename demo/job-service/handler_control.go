package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

var accounts = []string{
	"acc:001", "acc:002", "acc:003", "acc:004", "acc:005",
}

func (app *App) handleStart(w http.ResponseWriter, r *http.Request) {
	countStr := r.URL.Query().Get("count")
	count := 10000
	if countStr != "" {
		if n, err := strconv.Atoi(countStr); err == nil && n > 0 {
			count = n
		}
	}

	speedStr := r.URL.Query().Get("speed")
	speed := 100
	if speedStr != "" {
		if n, err := strconv.Atoi(speedStr); err == nil && n > 0 {
			speed = n
		}
	}

	app.mu.Lock()
	if app.generatorStop != nil {
		close(app.generatorStop)
	}
	app.generatorStop = make(chan struct{})
	stop := app.generatorStop
	app.mu.Unlock()

	go app.generateTransactions(count, speed, stop)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "started",
		"count":  count,
		"speed":  speed,
	})
}

func (app *App) handleStop(w http.ResponseWriter, r *http.Request) {
	app.mu.Lock()
	if app.generatorStop != nil {
		close(app.generatorStop)
		app.generatorStop = nil
	}
	app.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func (app *App) generateTransactions(total, speed int, stop chan struct{}) {
	log.Printf("generating %d transactions at ~%d/s", total, speed)

	interval := time.Second / time.Duration(speed)

	for i := 0; i < total; i++ {
		select {
		case <-stop:
			log.Printf("generator stopped at %d/%d", i, total)
			return
		default:
		}

		debit := accounts[i%len(accounts)]
		credit := accounts[(i+1)%len(accounts)]
		tx := TxEvent{
			ID:            fmt.Sprintf("tx-%06d", i+1),
			DebitAccount:  debit,
			CreditAccount: credit,
			Amount:        uint64((i%100 + 1) * 10),
			Currency:      "USD",
		}

		data, _ := json.Marshal(tx)
		if err := app.nc.Publish("tx.pending", data); err != nil {
			log.Printf("publish error: %v", err)
			continue
		}
		app.totalSent.Add(1)

		time.Sleep(interval)
	}

	log.Printf("generated %d transactions", total)
}
