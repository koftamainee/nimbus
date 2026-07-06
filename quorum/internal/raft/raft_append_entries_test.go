package raft

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendEntries_HeartbeatMatch(t *testing.T) {
	r := New("follower", nil, nil, nil, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.log = []LogEntry{{Index: 0, Term: 1}}
	r.currentTerm = 1
	r.mu.Unlock()

	resp := r.HandleAppendEntries(AppendRequest{
		Term:         1,
		LeaderID:     "leader",
		PrevLogIndex: 0,
		PrevLogTerm:  1,
		LeaderCommit: 0,
	})

	assert.True(t, resp.Success, "heartbeat should succeed")
	assert.Equal(t, 1, resp.Term)
}

func TestAppendEntries_LowerTerm(t *testing.T) {
	r := New("follower", nil, nil, nil, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.currentTerm = 2
	r.mu.Unlock()

	resp := r.HandleAppendEntries(AppendRequest{
		Term:     1,
		LeaderID: "leader",
	})

	assert.False(t, resp.Success)
	assert.Equal(t, 2, resp.Term)
}

func TestAppendEntries_HeartbeatEmptyLog(t *testing.T) {
	r := New("follower", nil, nil, nil, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.currentTerm = 1
	r.mu.Unlock()

	resp := r.HandleAppendEntries(AppendRequest{
		Term:         1,
		LeaderID:     "leader",
		PrevLogIndex: -1,
		PrevLogTerm:  0,
		LeaderCommit: 0,
	})

	assert.True(t, resp.Success)
}

func TestAppendEntries_LogConflictShorter(t *testing.T) {
	r := New("follower", nil, nil, nil, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.log = []LogEntry{{Index: 0, Term: 1}}
	r.currentTerm = 1
	r.mu.Unlock()

	resp := r.HandleAppendEntries(AppendRequest{
		Term:         1,
		LeaderID:     "leader",
		PrevLogIndex: 2,
		PrevLogTerm:  1,
		LeaderCommit: 0,
	})

	assert.False(t, resp.Success)
}

func TestAppendEntries_LogConflictTermMismatch(t *testing.T) {
	r := New("follower", nil, nil, nil, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.currentTerm = 2
	r.log = []LogEntry{
		{Index: 0, Term: 1},
		{Index: 1, Term: 2},
	}
	r.mu.Unlock()

	resp := r.HandleAppendEntries(AppendRequest{
		Term:         2,
		LeaderID:     "leader",
		PrevLogIndex: 1,
		PrevLogTerm:  1,
		LeaderCommit: 0,
	})

	assert.False(t, resp.Success)
}

func TestAppendEntries_ReplicateSingleEntry(t *testing.T) {
	r := New("follower", nil, nil, nil, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.currentTerm = 1
	r.mu.Unlock()

	resp := r.HandleAppendEntries(AppendRequest{
		Term:         1,
		LeaderID:     "leader",
		PrevLogIndex: -1,
		PrevLogTerm:  0,
		Entries: []LogEntry{
			{Index: 0, Term: 1, Command: []byte("SET foo bar")},
		},
		LeaderCommit: 0,
	})

	assert.True(t, resp.Success)

	r.mu.Lock()
	require.Len(t, r.log, 1)
	assert.Equal(t, 0, r.log[0].Index)
	assert.Equal(t, 1, r.log[0].Term)
	assert.Equal(t, "SET foo bar", string(r.log[0].Command))
	r.mu.Unlock()
}

func TestAppendEntries_ReplicateMultipleEntries(t *testing.T) {
	r := New("follower", nil, nil, nil, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.currentTerm = 1
	r.mu.Unlock()

	resp := r.HandleAppendEntries(AppendRequest{
		Term:         1,
		LeaderID:     "leader",
		PrevLogIndex: -1,
		PrevLogTerm:  0,
		Entries: []LogEntry{
			{Index: 0, Term: 1, Command: []byte("a")},
			{Index: 1, Term: 1, Command: []byte("b")},
		},
		LeaderCommit: 0,
	})

	assert.True(t, resp.Success)

	r.mu.Lock()
	require.Len(t, r.log, 2)
	assert.Equal(t, "a", string(r.log[0].Command))
	assert.Equal(t, "b", string(r.log[1].Command))
	r.mu.Unlock()
}

func TestAppendEntries_OverwriteConflictingEntry(t *testing.T) {
	r := New("follower", nil, nil, nil, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.currentTerm = 3
	r.log = []LogEntry{
		{Index: 0, Term: 1, Command: []byte("old")},
		{Index: 1, Term: 2, Command: []byte("conflict")},
	}
	r.mu.Unlock()

	resp := r.HandleAppendEntries(AppendRequest{
		Term:         3,
		LeaderID:     "leader",
		PrevLogIndex: 0,
		PrevLogTerm:  1,
		Entries: []LogEntry{
			{Index: 1, Term: 3, Command: []byte("correct")},
		},
		LeaderCommit: 0,
	})

	assert.True(t, resp.Success)

	r.mu.Lock()
	require.Len(t, r.log, 2)
	assert.Equal(t, "correct", string(r.log[1].Command))
	r.mu.Unlock()
}

func TestAppendEntries_AdvancesCommitIndex(t *testing.T) {
	r := New("follower", nil, nil, nil, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.currentTerm = 1
	r.log = []LogEntry{
		{Index: 0, Term: 1},
		{Index: 1, Term: 1},
	}
	r.commitIndex = 0
	r.mu.Unlock()

	resp := r.HandleAppendEntries(AppendRequest{
		Term:         1,
		LeaderID:     "leader",
		PrevLogIndex: 1,
		PrevLogTerm:  1,
		LeaderCommit: 1,
	})

	assert.True(t, resp.Success)

	r.mu.Lock()
	assert.Equal(t, 1, r.commitIndex)
	r.mu.Unlock()
}

func TestAppendEntries_CommitClamped(t *testing.T) {
	r := New("follower", nil, nil, nil, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.currentTerm = 1
	r.log = []LogEntry{
		{Index: 0, Term: 1},
	}
	r.commitIndex = 0
	r.mu.Unlock()

	resp := r.HandleAppendEntries(AppendRequest{
		Term:         1,
		LeaderID:     "leader",
		PrevLogIndex: 0,
		PrevLogTerm:  1,
		LeaderCommit: 5,
	})

	assert.True(t, resp.Success)

	r.mu.Lock()
	assert.Equal(t, 0, r.commitIndex, "commit must not exceed last log index")
	r.mu.Unlock()
}
