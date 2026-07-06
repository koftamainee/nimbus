package raft

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplicateLog_SendsHeartbeatToFollowers(t *testing.T) {
	leader, f1, _ := setupLeaderWithFollowers(t,
		[]LogEntry{{Index: 0, Term: 1, Command: []byte("x")}},
		[]LogEntry{{Index: 0, Term: 1, Command: []byte("x")}},
	)

	leader.mu.Lock()
	leader.nextIndex = []int{1, 1}
	leader.matchIndex = []int{0, 0}
	leader.mu.Unlock()

	leader.replicateLog()

	time.Sleep(50 * time.Millisecond)

	f1.mu.Lock()
	assert.Equal(t, 1, f1.currentTerm, "follower should stay in same term after heartbeat")
	assert.Equal(t, 1, len(f1.log))
	f1.mu.Unlock()
}

func TestReplicateLog_ReplicatesNewEntries(t *testing.T) {
	leader, f1, f2 := setupLeaderWithFollowers(t,
		[]LogEntry{
			{Index: 0, Term: 1, Command: []byte("x")},
			{Index: 1, Term: 1, Command: []byte("y")},
		},
		[]LogEntry{{Index: 0, Term: 1, Command: []byte("x")}},
	)

	leader.mu.Lock()
	leader.nextIndex = []int{1, 1}
	leader.matchIndex = []int{0, 0}
	leader.mu.Unlock()

	leader.replicateLog()

	time.Sleep(50 * time.Millisecond)

	f1.mu.Lock()
	require.Len(t, f1.log, 2)
	assert.Equal(t, "y", string(f1.log[1].Command))
	f1.mu.Unlock()

	f2.mu.Lock()
	require.Len(t, f2.log, 2)
	assert.Equal(t, "y", string(f2.log[1].Command))
	f2.mu.Unlock()

	leader.mu.Lock()
	assert.Equal(t, 1, leader.matchIndex[0], "matchIndex should reflect replicated entry")
	assert.Equal(t, 2, leader.nextIndex[0], "nextIndex should advance")
	assert.Equal(t, 1, leader.matchIndex[1])
	assert.Equal(t, 2, leader.nextIndex[1])
	leader.mu.Unlock()
}

func TestReplicateLog_DecrementsNextIndexOnConflict(t *testing.T) {
	leader, _, _ := setupLeaderWithFollowers(t,
		[]LogEntry{{Index: 0, Term: 1, Command: []byte("x")}},
		[]LogEntry{{Index: 0, Term: 1, Command: []byte("x")}},
	)

	leader.mu.Lock()
	leader.nextIndex = []int{5, 1}
	leader.matchIndex = []int{4, 0}
	leader.mu.Unlock()

	for i := 0; i < 5; i++ {
		leader.replicateLog()
		time.Sleep(20 * time.Millisecond)
	}

	leader.mu.Lock()
	assert.Less(t, leader.nextIndex[0], 5, "nextIndex should be decremented after conflicts")
	leader.mu.Unlock()
}

func TestReplicateLog_AdvancesCommitIndex(t *testing.T) {
	leader, f1, f2 := setupLeaderWithFollowers(t,
		[]LogEntry{
			{Index: 0, Term: 1, Command: []byte("x")},
			{Index: 1, Term: 1, Command: []byte("y")},
			{Index: 2, Term: 1, Command: []byte("z")},
		},
		[]LogEntry{
			{Index: 0, Term: 1, Command: []byte("x")},
			{Index: 1, Term: 1, Command: []byte("y")},
			{Index: 2, Term: 1, Command: []byte("z")},
		},
	)

	leader.mu.Lock()
	leader.nextIndex = []int{3, 3}
	leader.matchIndex = []int{2, 2}
	leader.commitIndex = 1
	leader.mu.Unlock()

	f1.mu.Lock()
	f1.commitIndex = 2
	f1.mu.Unlock()

	f2.mu.Lock()
	f2.commitIndex = 2
	f2.mu.Unlock()

	leader.replicateLog()

	time.Sleep(50 * time.Millisecond)

	leader.mu.Lock()
	assert.Equal(t, 2, leader.commitIndex, "commitIndex should advance when majority has entries")
	leader.mu.Unlock()
}

func TestReplicateLog_StepsDownOnStaleTerm(t *testing.T) {
	leader, f1, _ := setupLeaderWithFollowers(t,
		[]LogEntry{{Index: 0, Term: 2, Command: []byte("x")}},
		[]LogEntry{{Index: 0, Term: 2, Command: []byte("x")}},
	)

	leader.mu.Lock()
	leader.currentTerm = 2
	leader.nextIndex = []int{1, 1}
	leader.matchIndex = []int{0, 0}
	leader.mu.Unlock()

	f1.mu.Lock()
	f1.currentTerm = 3
	f1.votedFor = ""
	f1.mu.Unlock()

	leader.replicateLog()

	time.Sleep(50 * time.Millisecond)

	leader.mu.Lock()
	assert.Equal(t, Follower, leader.role, "leader should step down on stale term")
	assert.Equal(t, 3, leader.currentTerm, "should update to higher term")
	leader.mu.Unlock()
}
