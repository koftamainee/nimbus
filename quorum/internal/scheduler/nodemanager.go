package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	quorumv1 "quorum/gen/quorum/v1"
)

type NodeManager struct {
	kvClient    quorumv1.KVClient
	watchClient quorumv1.WatchClient
	mu          sync.RWMutex
	alive       map[string]time.Time
	deadline    time.Duration
	nodeID      string
	stop        chan struct{}
}

func NewNodeManager(kv quorumv1.KVClient, wc quorumv1.WatchClient, nodeID string) *NodeManager {
	return &NodeManager{
		kvClient:    kv,
		watchClient: wc,
		alive:       make(map[string]time.Time),
		deadline:    NodeDeadTimeout * time.Second,
		nodeID:      nodeID,
		stop:        make(chan struct{}),
	}
}

func (nm *NodeManager) Run(ctx context.Context) {
	for {
		if nm.isLeader(ctx) {
			nm.runActive(ctx)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-nm.stop:
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (nm *NodeManager) runActive(ctx context.Context) {
	slog.Info("node manager active (leader)")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go nm.watchNodes(ctx)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-nm.stop:
			return
		case <-ticker.C:
			nm.evictStale()
		}
	}
}

func (nm *NodeManager) watchNodes(ctx context.Context) {
	for {
		stream, err := nm.watchClient.Watch(ctx, &quorumv1.WatchRequest{Key: []byte("/nodes/")})
		if err != nil {
			st, ok := status.FromError(err)
			if ok && st.Code() == codes.Unavailable && strings.HasPrefix(st.Message(), "leader: ") {
				slog.Warn("node manager leader changed, reconnecting", "msg", st.Message())
				time.Sleep(time.Second)
				continue
			}
			slog.Error("node manager watch failed", "error", err)
			time.Sleep(time.Second)
			continue
		}

		for {
			resp, err := stream.Recv()
			if err != nil {
				slog.Warn("node manager watch stream broken", "error", err)
				time.Sleep(time.Second)
				break
			}
			nm.handleWatchEvents(resp.Events)
		}
	}
}

func (nm *NodeManager) handleWatchEvents(events []*quorumv1.Event) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	now := time.Now()
	for _, e := range events {
		key := string(e.Key)

		switch e.Type {
		case quorumv1.Event_PUT:
			if strings.HasSuffix(key, "/heartbeat") {
				nm.handleHeartbeat(key, e.Value, now)
			} else if strings.Count(key, "/") == 2 {
				nm.handleRegistration(key, e.Value)
			}

		case quorumv1.Event_DELETE:
			if strings.Count(key, "/") == 2 {
				nodeID := key[len("/nodes/"):]
				delete(nm.alive, nodeID)
				slog.Info("node removed", "node", nodeID)
			}
		}
	}
}

func (nm *NodeManager) handleHeartbeat(key string, value []byte, now time.Time) {
	parts := strings.SplitN(key, "/", 4)
	if len(parts) < 3 {
		return
	}
	nodeID := parts[2]

	var hb Heartbeat
	if err := json.Unmarshal(value, &hb); err != nil {
		delete(nm.alive, nodeID)
		return
	}

	parsed, err := time.Parse(time.RFC3339, hb.Time)
	if err != nil {
		delete(nm.alive, nodeID)
		return
	}

	if now.Sub(parsed) < nm.deadline {
		nm.alive[nodeID] = parsed
	} else {
		delete(nm.alive, nodeID)
	}
}

func (nm *NodeManager) handleRegistration(key string, value []byte) {
	nodeID := key[len("/nodes/"):]
	_ = value
	slog.Debug("node registered", "node", nodeID)
}

func (nm *NodeManager) evictStale() {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	now := time.Now()
	for id, t := range nm.alive {
		if now.Sub(t) >= nm.deadline {
			slog.Info("node dead", "node", id, "last_heartbeat", t.Format(time.RFC3339))
			delete(nm.alive, id)
		}
	}
}

func (nm *NodeManager) isLeader(ctx context.Context) bool {
	_, err := nm.kvClient.Put(ctx, &quorumv1.PutRequest{
		Key:   []byte("/internal/scheduler/lease"),
		Value: []byte(nm.nodeID),
	})
	return err == nil
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
