package raft

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startTestNode(t *testing.T, id string, peers []string, transport Transport) *Raft {
	t.Helper()
	r := New(id, peers, transport)
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
		r := New(id, peerIDs(ids, id), transport)
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

func TestNewRaft(t *testing.T) {
	r := New("node-1", []string{"node-2", "node-3"}, nil)

	r.mu.Lock()
	assert.Equal(t, Follower, r.role)
	assert.Equal(t, 0, r.currentTerm)
	assert.Equal(t, "", r.votedFor)
	assert.Empty(t, r.log)
	assert.Equal(t, 0, r.commitIndex)
	assert.Equal(t, 0, r.lastApplied)
	assert.Equal(t, "node-1", r.id)
	assert.Len(t, r.peers, 2)
	r.mu.Unlock()
}

func TestFollowerDoesntStartElectionBeforeTimeout(t *testing.T) {
	r := New("node-1", []string{"node-2", "node-3"}, nil)
	go r.Run()
	time.Sleep(125 * time.Millisecond)

	r.mu.Lock()
	assert.Equal(t, Follower, r.role)
	assert.Equal(t, 0, r.currentTerm)
	r.mu.Unlock()
}

func TestFollowerBecomesCandidateAfterTimeout(t *testing.T) {
	r := New("node-1", []string{"node-2", "node-3"}, nil)
	go r.Run()
	time.Sleep(350 * time.Millisecond)

	r.mu.Lock()
	assert.Equal(t, Candidate, r.role)
	assert.GreaterOrEqual(t, r.currentTerm, 1)
	assert.Equal(t, r.id, r.votedFor)
	r.mu.Unlock()
}

func TestHandleRequestVoteGrant(t *testing.T) {
	r := startTestNode(t, "node-1", []string{"node-2", "node-3"}, nil)

	resp := r.HandleRequestVote(VoteRequest{
		Term:         1,
		CandidateID:  "node-2",
		LastLogIndex: 0,
		LastLogTerm:  0,
	})
	assert.True(t, resp.VoteGranted)
	assert.Equal(t, 1, resp.Term)

	r.mu.Lock()
	assert.Equal(t, "node-2", r.votedFor)
	r.mu.Unlock()
}

func TestHandleRequestVoteAlreadyVoted(t *testing.T) {
	r := startTestNode(t, "node-1", []string{"node-2", "node-3"}, nil)

	r.HandleRequestVote(VoteRequest{
		Term:         1,
		CandidateID:  "node-2",
		LastLogIndex: 0,
		LastLogTerm:  0,
	})

	resp := r.HandleRequestVote(VoteRequest{
		Term:         1,
		CandidateID:  "node-3",
		LastLogIndex: 0,
		LastLogTerm:  0,
	})
	assert.False(t, resp.VoteGranted)
}

func TestHandleRequestVoteSameCandidateIdempotency(t *testing.T) {
	r := startTestNode(t, "node-1", []string{"node-2", "node-3"}, nil)

	r.HandleRequestVote(VoteRequest{
		Term:         1,
		CandidateID:  "node-2",
		LastLogIndex: 0,
		LastLogTerm:  0,
	})

	resp := r.HandleRequestVote(VoteRequest{
		Term:         1,
		CandidateID:  "node-2",
		LastLogIndex: 0,
		LastLogTerm:  0,
	})
	assert.True(t, resp.VoteGranted)
}

func TestHandleRequestVoteLowerTerm(t *testing.T) {
	r := startTestNode(t, "node-1", []string{"node-2", "node-3"}, nil)

	r.mu.Lock()
	r.currentTerm = 2
	r.mu.Unlock()

	resp := r.HandleRequestVote(VoteRequest{
		Term:         1,
		CandidateID:  "node-2",
		LastLogIndex: 0,
		LastLogTerm:  0,
	})
	assert.False(t, resp.VoteGranted)
	assert.Equal(t, 2, resp.Term)
}

func TestThreeNodesElectLeader(t *testing.T) {
	nodes, _ := newCluster(t, []string{"n1", "n2", "n3"})
	time.Sleep(2 * time.Second)

	leaders := 0
	var leaderID string
	for id, r := range nodes {
		r.mu.Lock()
		if r.role == Leader {
			leaders++
			leaderID = id
		}
		r.mu.Unlock()
	}

	require.Equal(t, 1, leaders, "must have exactly one leader")
	require.NotEmpty(t, leaderID)

	for id, r := range nodes {
		r.mu.Lock()
		if id != leaderID {
			assert.NotEqualf(t, Leader, r.role, "node %s should not be leader", id)
		}
		r.mu.Unlock()
	}
}

func TestHandleRequestVoteLogMoreUpToDate(t *testing.T) {
	r := startTestNode(t, "node-1", []string{"node-2", "node-3"}, nil)

	r.mu.Lock()
	r.currentTerm = 1
	r.log = []LogEntry{
		{Index: 0, Term: 1, Command: []byte("data")},
		{Index: 1, Term: 2, Command: []byte("y")},
	}
	r.mu.Unlock()

	resp := r.HandleRequestVote(VoteRequest{
		Term:         2,
		CandidateID:  "node-2",
		LastLogIndex: 0,
		LastLogTerm:  1,
	})
	assert.False(t, resp.VoteGranted)
}
