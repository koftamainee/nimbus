package raft

import (
	"sync"
	"testing"
	"time"
)

func startTestNode(t *testing.T, id string, peers []string, transport Transport) *Raft {
	t.Helper()
	r := New(id, peers, transport, nil, nil, nil)
	go r.Run()
	<-r.ready
	return r
}

func peerIDs(ids []string, self string) []string {
	var peers []string
	for _, id := range ids {
		if id != self {
			peers = append(peers, id)
		}
	}
	return peers
}

func newCluster(t *testing.T, ids []string) (map[string]*Raft, *InMemoryTransport) {
	t.Helper()
	nodes := make(map[string]*Raft, len(ids))
	transport := &InMemoryTransport{NodesByID: nodes}

	for _, id := range ids {
		r := New(id, peerIDs(ids, id), transport, nil, nil, nil)
		nodes[id] = r
	}

	for _, r := range nodes {
		go r.Run()
	}

	for _, r := range nodes {
		<-r.ready
	}

	return nodes, transport
}

func setupLeaderWithFollowers(t *testing.T, leaderLog, followerLog []LogEntry) (*Raft, *Raft, *Raft) {
	t.Helper()

	leader := New("leader", []string{"f1", "f2"}, nil, nil, nil, nil)
	leader.electionTimer = time.NewTimer(time.Hour)
	leader.heartbeatTimer = time.NewTimer(time.Hour)
	leader.mu.Lock()
	leader.role = Leader
	leader.log = leaderLog
	leader.currentTerm = 1
	leader.commitIndex = 0
	leader.mu.Unlock()

	f1 := New("f1", nil, nil, nil, nil, nil)
	f1.electionTimer = time.NewTimer(time.Hour)
	f1.mu.Lock()
	f1.log = followerLog
	f1.currentTerm = 1
	f1.commitIndex = 0
	f1.mu.Unlock()

	f2 := New("f2", nil, nil, nil, nil, nil)
	f2.electionTimer = time.NewTimer(time.Hour)
	f2.mu.Lock()
	f2.log = followerLog
	f2.currentTerm = 1
	f2.commitIndex = 0
	f2.mu.Unlock()

	transport := InMemoryTransport{NodesByID: map[string]*Raft{
		"leader": leader,
		"f1":     f1,
		"f2":     f2,
	}}
	leader.transport = transport

	return leader, f1, f2
}

type recordingSM struct {
	mu   sync.Mutex
	cmds [][]byte
}

func (m *recordingSM) Apply(cmd []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cmds = append(m.cmds, cmd)
}

func (m *recordingSM) Applied() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make([][]byte, len(m.cmds))
	copy(res, m.cmds)
	return res
}

func (m *recordingSM) Snapshot() ([]byte, error) {
	return []byte("snapshot"), nil
}

func (m *recordingSM) Restore(_ []byte) error {
	return nil
}
