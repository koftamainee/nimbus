package raft

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyCommitted_AppliesSingleEntry(t *testing.T) {
	sm := &recordingSM{}
	r := New("n1", nil, nil, sm, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.log = []LogEntry{
		{Index: 0, Term: 1, Command: []byte("cmd1")},
	}
	r.commitIndex = 0
	r.lastApplied = -1
	r.mu.Unlock()

	r.applyCommitted()

	applied := sm.Applied()
	require.Len(t, applied, 1)
	assert.Equal(t, "cmd1", string(applied[0]))

	r.mu.Lock()
	assert.Equal(t, 0, r.lastApplied)
	r.mu.Unlock()
}

func TestApplyCommitted_AppliesMultipleUntilCommit(t *testing.T) {
	sm := &recordingSM{}
	r := New("n1", nil, nil, sm, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.log = []LogEntry{
		{Index: 0, Term: 1, Command: []byte("a")},
		{Index: 1, Term: 1, Command: []byte("b")},
		{Index: 2, Term: 1, Command: []byte("c")},
	}
	r.commitIndex = 1
	r.lastApplied = -1
	r.mu.Unlock()

	r.applyCommitted()

	applied := sm.Applied()
	require.Len(t, applied, 2)
	assert.Equal(t, "a", string(applied[0]))
	assert.Equal(t, "b", string(applied[1]))

	r.mu.Lock()
	assert.Equal(t, 1, r.lastApplied)
	r.mu.Unlock()
}

func TestApplyCommitted_SkipsAlreadyApplied(t *testing.T) {
	sm := &recordingSM{}
	r := New("n1", nil, nil, sm, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.log = []LogEntry{
		{Index: 0, Term: 1, Command: []byte("a")},
		{Index: 1, Term: 1, Command: []byte("b")},
	}
	r.commitIndex = 1
	r.lastApplied = 0
	r.mu.Unlock()

	r.applyCommitted()

	applied := sm.Applied()
	require.Len(t, applied, 1)
	assert.Equal(t, "b", string(applied[0]))

	r.mu.Lock()
	assert.Equal(t, 1, r.lastApplied)
	r.mu.Unlock()
}

func TestApplyCommitted_WithoutStateMachine(t *testing.T) {
	r := New("n1", nil, nil, nil, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.log = []LogEntry{{Index: 0, Term: 1, Command: []byte("x")}}
	r.commitIndex = 0
	r.lastApplied = -1
	r.mu.Unlock()

	r.applyCommitted()

	r.mu.Lock()
	assert.Equal(t, 0, r.lastApplied, "lastApplied advances even without state machine")
	r.mu.Unlock()
}

func TestApplyCommitted_LeaderAppliesAfterReplicate(t *testing.T) {
	nodes, _ := newCluster(t, []string{"n1", "n2", "n3"})

	for _, r := range nodes {
		r.SetStateMachine(&recordingSM{})
	}

	time.Sleep(2 * time.Second)

	var leader *Raft
	for _, r := range nodes {
		r.mu.Lock()
		if r.role == Leader {
			leader = r
		}
		r.mu.Unlock()
	}
	require.NotNil(t, leader)

	err := leader.Propose(context.Background(), []byte("SET x 1"))
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	var leaderSM *recordingSM
	leader.mu.Lock()
	if sm, ok := leader.stateMachine.(*recordingSM); ok {
		leaderSM = sm
	}
	leader.mu.Unlock()
	require.NotNil(t, leaderSM)

	applied := leaderSM.Applied()
	require.GreaterOrEqual(t, len(applied), 1, "leader should eventually apply its own proposal")
	assert.Equal(t, "SET x 1", string(applied[0]))
}
