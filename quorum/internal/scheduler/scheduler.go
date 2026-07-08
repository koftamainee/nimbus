package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	quorumv1 "quorum/gen/quorum/v1"
)

type Scheduler struct {
	kvClient    quorumv1.KVClient
	watchClient quorumv1.WatchClient
	nm          *NodeManager
	nodeID      string
	trigger     chan struct{}
	stop        chan struct{}
}

func New(kv quorumv1.KVClient, wc quorumv1.WatchClient, nm *NodeManager, nodeID string) *Scheduler {
	return &Scheduler{
		kvClient:    kv,
		watchClient: wc,
		nm:          nm,
		nodeID:      nodeID,
		trigger:     make(chan struct{}, 1),
		stop:        make(chan struct{}),
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	for {
		if s.isLeader(ctx) {
			s.runActive(ctx)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (s *Scheduler) runActive(ctx context.Context) {
	slog.Info("scheduler active (leader)")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go s.watchDesired(ctx)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	s.reconcile(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-s.trigger:
			s.reconcile(ctx)
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

func (s *Scheduler) watchDesired(ctx context.Context) {
	for {
		stream, err := s.watchClient.Watch(ctx, &quorumv1.WatchRequest{Key: []byte(DesiredPrefix)})
		if err != nil {
			st, ok := status.FromError(err)
			if ok && st.Code() == codes.Unavailable && strings.HasPrefix(st.Message(), "leader: ") {
				slog.Warn("scheduler leader changed, reconnecting", "msg", st.Message())
				time.Sleep(time.Second)
				continue
			}
			slog.Error("scheduler watch failed", "error", err)
			time.Sleep(time.Second)
			continue
		}

		for {
			resp, err := stream.Recv()
			if err != nil {
				slog.Warn("scheduler watch stream broken", "error", err)
				time.Sleep(time.Second)
				break
			}
			if len(resp.Events) > 0 {
				select {
				case s.trigger <- struct{}{}:
				default:
				}
			}
		}
	}
}

func (s *Scheduler) isLeader(ctx context.Context) bool {
	_, err := s.kvClient.Put(ctx, &quorumv1.PutRequest{
		Key:   []byte("/internal/scheduler/lease"),
		Value: []byte(s.nodeID),
	})
	return err == nil
}

func (s *Scheduler) Stop() {
	close(s.stop)
}

func (s *Scheduler) reconcile(ctx context.Context) {
	desired := s.listDesired(ctx)
	aliveNodes := s.nm.AliveNodes()

	for _, dc := range desired {
		assignments := s.listAssignmentsForContainer(ctx, dc.Name)
		running := 0
		for _, a := range assignments {
			if a.Status == "assigned" || a.Status == "running" {
				running++
			}
		}

		if running < dc.Replicas {
			needed := dc.Replicas - running
			usedNodes := make(map[string]bool)
			for _, a := range assignments {
				if a.Status == "assigned" || a.Status == "running" {
					usedNodes[a.NodeID] = true
				}
			}
			nextIdx := nextIndex(assignments, dc.Name)
			for i := 0; i < needed; i++ {
				if len(aliveNodes) == 0 {
					break
				}
				candidates := aliveNodes
				if len(usedNodes) < len(aliveNodes) {
					candidates = nil
					for _, n := range aliveNodes {
						if !usedNodes[n] {
							candidates = append(candidates, n)
						}
					}
				}
				node := s.pickNode(candidates, s.assignmentCountByNode(ctx))
				if node == "" {
					break
				}
				usedNodes[node] = true
				containerName := fmt.Sprintf("%s-%d", dc.Name, nextIdx)
				s.createAssignment(ctx, node, dc, containerName)
				nextIdx++
			}
		}

		if running > dc.Replicas {
			excess := running - dc.Replicas
			sort.Slice(assignments, func(i, j int) bool {
				iIdx := parseIndex(assignments[i].ContainerName, dc.Name)
				jIdx := parseIndex(assignments[j].ContainerName, dc.Name)
				return iIdx > jIdx
			})
			for _, a := range assignments {
				if excess <= 0 {
					break
				}
				if a.Status == "assigned" || a.Status == "running" {
					s.removeAssignment(ctx, a.NodeID, a.ContainerName)
					excess--
				}
			}
		}

		s.rescheduleFromDeadNodes(ctx, dc, aliveNodes)
	}
}

func (s *Scheduler) rescheduleFromDeadNodes(ctx context.Context, dc DesiredContainer, aliveNodes []string) {
	assignments := s.listAssignmentsForContainer(ctx, dc.Name)
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
		s.removeAssignment(ctx, a.NodeID, a.ContainerName)
	}
}

func (s *Scheduler) listDesired(ctx context.Context) []DesiredContainer {
	resp, err := s.kvClient.Range(ctx, &quorumv1.RangeRequest{
		Key:      []byte(DesiredPrefix),
		RangeEnd: []byte(prefixEnd(DesiredPrefix)),
	})
	if err != nil {
		return nil
	}

	var result []DesiredContainer
	for _, kv := range resp.Kvs {
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
	return s.nm.AliveNodes()
}

func (s *Scheduler) listAssignmentsForContainer(ctx context.Context, name string) []Assignment {
	nodeResp, err := s.kvClient.Range(ctx, &quorumv1.RangeRequest{
		Key:      []byte("/nodes/"),
		RangeEnd: []byte(prefixEnd("/nodes/")),
	})
	if err != nil {
		return nil
	}

	var result []Assignment
	for _, nkv := range nodeResp.Kvs {
		nodeID := string(nkv.Key)
		nodeID = nodeID[len("/nodes/"):]
		prefix := fmt.Sprintf(AssignPrefix, nodeID)
		akvs, err := s.kvClient.Range(ctx, &quorumv1.RangeRequest{
			Key:      []byte(prefix),
			RangeEnd: []byte(prefixEnd(prefix)),
		})
		if err != nil {
			continue
		}
		for _, akv := range akvs.Kvs {
			var a Assignment
			if err := json.Unmarshal(akv.Value, &a); err != nil {
				continue
			}
			if strings.HasPrefix(a.ContainerName, name+"-") {
				result = append(result, a)
			}
		}
	}
	return result
}

func (s *Scheduler) assignmentCountByNode(ctx context.Context) map[string]int {
	counts := make(map[string]int)
	nodeResp, err := s.kvClient.Range(ctx, &quorumv1.RangeRequest{
		Key:      []byte("/nodes/"),
		RangeEnd: []byte(prefixEnd("/nodes/")),
	})
	if err != nil {
		return counts
	}

	for _, nkv := range nodeResp.Kvs {
		nodeID := string(nkv.Key)
		nodeID = nodeID[len("/nodes/"):]
		prefix := fmt.Sprintf(AssignPrefix, nodeID)
		akvs, err := s.kvClient.Range(ctx, &quorumv1.RangeRequest{
			Key:      []byte(prefix),
			RangeEnd: []byte(prefixEnd(prefix)),
		})
		if err != nil {
			continue
		}
		for _, akv := range akvs.Kvs {
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

func (s *Scheduler) createAssignment(ctx context.Context, nodeID string, dc DesiredContainer, containerName string) {
	a := Assignment{
		ContainerName: containerName,
		Spec:          dc,
		NodeID:        nodeID,
		Status:        "assigned",
	}
	data, _ := json.Marshal(a)
	key := fmt.Sprintf(AssignKey, nodeID, containerName)

	_, err := s.kvClient.Put(ctx, &quorumv1.PutRequest{
		Key: []byte(key), Value: data,
	})
	if err != nil {
		slog.Warn("failed to create assignment", "container", containerName, "node", nodeID, "error", err)
	}
}

func (s *Scheduler) removeAssignment(ctx context.Context, nodeID, containerName string) {
	key := fmt.Sprintf(AssignKey, nodeID, containerName)
	_, err := s.kvClient.Delete(ctx, &quorumv1.DeleteRequest{Key: []byte(key)})
	if err != nil {
		slog.Warn("failed to remove assignment", "container", containerName, "error", err)
	}
	s.kvClient.Delete(ctx, &quorumv1.DeleteRequest{
		Key: []byte(fmt.Sprintf("/containers/%s/status", containerName)),
	})
}

func parseIndex(containerName, baseName string) int {
	if !strings.HasPrefix(containerName, baseName+"-") {
		return 0
	}
	i, err := strconv.Atoi(containerName[len(baseName)+1:])
	if err != nil {
		return 0
	}
	return i
}

func nextIndex(assignments []Assignment, baseName string) int {
	maxIdx := 0
	for _, a := range assignments {
		if idx := parseIndex(a.ContainerName, baseName); idx > maxIdx {
			maxIdx = idx
		}
	}
	return maxIdx + 1
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
