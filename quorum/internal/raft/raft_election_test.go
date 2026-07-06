package raft

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRaft(t *testing.T) {
	r := New("node-1", []string{"node-2", "node-3"}, nil, nil, nil, nil)

	r.mu.Lock()
	assert.Equal(t, Follower, r.role)
	assert.Equal(t, 0, r.currentTerm)
	assert.Equal(t, "", r.votedFor)
	assert.Empty(t, r.log)
	assert.Equal(t, 0, r.commitIndex)
	assert.Equal(t, -1, r.lastApplied)
	assert.Equal(t, "node-1", r.id)
	assert.Len(t, r.peers, 2)
	r.mu.Unlock()
}

func TestFollowerDoesntStartElectionBeforeTimeout(t *testing.T) {
	r := New("node-1", []string{"node-2", "node-3"}, nil, nil, nil, nil)
	go r.Run()
	time.Sleep(125 * time.Millisecond)

	r.mu.Lock()
	assert.Equal(t, Follower, r.role)
	assert.Equal(t, 0, r.currentTerm)
	r.mu.Unlock()
}

func TestFollowerBecomesCandidateAfterTimeout(t *testing.T) {
	r := New("node-1", []string{"node-2", "node-3"}, nil, nil, nil, nil)
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
