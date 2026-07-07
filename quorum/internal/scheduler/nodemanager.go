package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"quorum/internal/store"
)

type NodeManager struct {
	store    *store.Store
	mu       sync.RWMutex
	alive    map[string]time.Time
	deadline time.Duration
	stop     chan struct{}
}

func NewNodeManager(st *store.Store) *NodeManager {
	return &NodeManager{
		store:    st,
		alive:    make(map[string]time.Time),
		deadline: NodeDeadTimeout * time.Second,
		stop:     make(chan struct{}),
	}
}

func (nm *NodeManager) Run(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-nm.stop:
			return
		case <-ticker.C:
			nm.checkHeartbeats()
		}
	}
}

func (nm *NodeManager) Stop() {
	close(nm.stop)
}

func (nm *NodeManager) IsAlive(nodeID string) bool {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	t, ok := nm.alive[nodeID]
	if !ok {
		return false
	}
	return time.Since(t) < nm.deadline
}

func (nm *NodeManager) AliveNodes() []string {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	var nodes []string
	now := time.Now()
	for id, t := range nm.alive {
		if now.Sub(t) < nm.deadline {
			nodes = append(nodes, id)
		}
	}
	return nodes
}

func (nm *NodeManager) checkHeartbeats() {
	nodeKVs, _ := nm.store.Range("/nodes/", prefixEnd("/nodes/"), 0)
	now := time.Now()

	for _, kv := range nodeKVs {
		nodeID := string(kv.Key)
		nodeID = nodeID[len("/nodes/"):]

		hbKey := fmt.Sprintf(HeartbeatKey, nodeID)
		hbVal, _, ok := nm.store.Get(hbKey)
		if !ok {
			nm.mu.Lock()
			delete(nm.alive, nodeID)
			nm.mu.Unlock()
			continue
		}

		var hb Heartbeat
		if err := json.Unmarshal([]byte(hbVal), &hb); err != nil {
			nm.mu.Lock()
			delete(nm.alive, nodeID)
			nm.mu.Unlock()
			continue
		}

		parsed, err := time.Parse(time.RFC3339, hb.Time)
		if err != nil {
			nm.mu.Lock()
			delete(nm.alive, nodeID)
			nm.mu.Unlock()
			continue
		}

		nm.mu.Lock()
		nm.alive[nodeID] = parsed
		nm.mu.Unlock()
	}

	nm.mu.RLock()
	for id, t := range nm.alive {
		if now.Sub(t) >= nm.deadline {
			slog.Info("node dead", "node", id, "last_heartbeat", t.Format(time.RFC3339))
		}
	}
	nm.mu.RUnlock()
}
