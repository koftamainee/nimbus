package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	quorumv1 "quorum/gen/quorum/v1"
	"quorum/internal/raft"
	"quorum/internal/store"
)

type Scheduler struct {
	raft  *raft.Raft
	store *store.Store
	nm    *NodeManager
	stop  chan struct{}
}

func New(r *raft.Raft, st *store.Store, nm *NodeManager) *Scheduler {
	return &Scheduler{
		raft:  r,
		store: st,
		nm:    nm,
		stop:  make(chan struct{}),
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-ticker.C:
			s.reconcile()
		}
	}
}

func (s *Scheduler) Stop() {
	close(s.stop)
}

func (s *Scheduler) reconcile() {
	desired := s.listDesired()
	aliveNodes := s.listAliveNodes()

	for _, dc := range desired {
		assignments := s.listAssignmentsForContainer(dc.Name)
		running := 0
		for _, a := range assignments {
			if a.Status == "assigned" || a.Status == "running" {
				running++
			}
		}

		if running < dc.Replicas {
			needed := dc.Replicas - running
			for i := 0; i < needed; i++ {
				if len(aliveNodes) == 0 {
					break
				}
				node := s.pickNode(aliveNodes, s.assignmentCountByNode())
				if node == "" {
					break
				}
				s.createAssignment(node, dc)
			}
		}

		if running > dc.Replicas {
			excess := running - dc.Replicas
			for _, a := range assignments {
				if excess <= 0 {
					break
				}
				if a.Status == "assigned" || a.Status == "running" {
					s.removeAssignment(a.NodeID, a.ContainerName)
					excess--
				}
			}
		}

		s.rescheduleFromDeadNodes(dc, aliveNodes)
	}
}

func (s *Scheduler) rescheduleFromDeadNodes(dc DesiredContainer, aliveNodes []string) {
	assignments := s.listAssignmentsForContainer(dc.Name)
	for _, a := range assignments {
		if a.Status != "assigned" && a.Status != "running" {
			continue
		}
		alive := false
		for _, n := range aliveNodes {
			if n == a.NodeID {
				alive = true
				break
			}
		}
		if alive {
			continue
		}
		slog.Info("rescheduling container from dead node",
			"container", dc.Name, "node", a.NodeID)
		s.removeAssignment(a.NodeID, a.ContainerName)
	}
}

func (s *Scheduler) listDesired() []DesiredContainer {
	kvs, _ := s.store.Range(DesiredPrefix, prefixEnd(DesiredPrefix), 0)
	var result []DesiredContainer
	for _, kv := range kvs {
		key := string(kv.Key)
		if !strings.HasSuffix(key, "/spec") {
			continue
		}
		var dc DesiredContainer
		if err := json.Unmarshal(kv.Value, &dc); err != nil {
			continue
		}
		result = append(result, dc)
	}
	return result
}

func (s *Scheduler) listAliveNodes() []string {
	nodeKVs, _ := s.store.Range("/nodes/", prefixEnd("/nodes/"), 0)
	var alive []string
	for _, kv := range nodeKVs {
		id := string(kv.Key)
		id = id[len("/nodes/"):]
		if s.nm.IsAlive(id) {
			alive = append(alive, id)
		}
	}
	return alive
}

func (s *Scheduler) listAssignmentsForContainer(name string) []Assignment {
	nodeKVs, _ := s.store.Range("/nodes/", prefixEnd("/nodes/"), 0)
	var result []Assignment
	for _, nkv := range nodeKVs {
		nodeID := string(nkv.Key)
		nodeID = nodeID[len("/nodes/"):]
		prefix := fmt.Sprintf(AssignPrefix, nodeID)
		akvs, _ := s.store.Range(prefix, prefixEnd(prefix), 0)
		for _, akv := range akvs {
			var a Assignment
			if err := json.Unmarshal(akv.Value, &a); err != nil {
				continue
			}
			if a.ContainerName == name {
				result = append(result, a)
			}
		}
	}
	return result
}

func (s *Scheduler) assignmentCountByNode() map[string]int {
	counts := make(map[string]int)
	nodeKVs, _ := s.store.Range("/nodes/", prefixEnd("/nodes/"), 0)
	for _, nkv := range nodeKVs {
		nodeID := string(nkv.Key)
		nodeID = nodeID[len("/nodes/"):]
		prefix := fmt.Sprintf(AssignPrefix, nodeID)
		akvs, _ := s.store.Range(prefix, prefixEnd(prefix), 0)
		for _, akv := range akvs {
			var a Assignment
			if err := json.Unmarshal(akv.Value, &a); err != nil {
				continue
			}
			if a.Status == "assigned" || a.Status == "running" {
				counts[nodeID]++
			}
		}
	}
	return counts
}

func (s *Scheduler) pickNode(nodes []string, counts map[string]int) string {
	if len(nodes) == 0 {
		return ""
	}
	sort.Slice(nodes, func(i, j int) bool {
		return counts[nodes[i]] < counts[nodes[j]]
	})
	return nodes[0]
}

func (s *Scheduler) createAssignment(nodeID string, dc DesiredContainer) {
	a := Assignment{
		ContainerName: dc.Name,
		Spec:          dc,
		NodeID:        nodeID,
		Status:        "assigned",
	}
	data, _ := json.Marshal(a)
	key := fmt.Sprintf(AssignKey, nodeID, dc.Name)

	cmd := &quorumv1.InternalRaftRequest{
		Cmd: &quorumv1.InternalRaftRequest_Put{
			Put: &quorumv1.PutRequest{Key: []byte(key), Value: data},
		},
	}
	cmdData, _ := proto.Marshal(cmd)
	if err := s.raft.Propose(context.Background(), cmdData); err != nil {
		slog.Warn("failed to create assignment", "container", dc.Name, "node", nodeID, "error", err)
	}
}

func (s *Scheduler) removeAssignment(nodeID, containerName string) {
	key := fmt.Sprintf(AssignKey, nodeID, containerName)
	cmd := &quorumv1.InternalRaftRequest{
		Cmd: &quorumv1.InternalRaftRequest_Delete{
			Delete: &quorumv1.DeleteRequest{Key: []byte(key)},
		},
	}
	cmdData, _ := proto.Marshal(cmd)
	if err := s.raft.Propose(context.Background(), cmdData); err != nil {
		slog.Warn("failed to remove assignment", "container", containerName, "error", err)
	}
}

func prefixEnd(prefix string) string {
	b := []byte(prefix)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < 0xFF {
			b[i]++
			return string(b[:i+1])
		}
	}
	return string(append(b, 0))
}
