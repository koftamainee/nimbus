package raft

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPropose_FollowerRejects(t *testing.T) {
	r := New("node-1", []string{"node-2"}, nil, nil, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)

	err := r.Propose(context.Background(), []byte("SET foo bar"))
	assert.Error(t, err)
	var target ErrNotLeader
	assert.ErrorAs(t, err, &target)
}

func TestPropose_CandidateRejects(t *testing.T) {
	r := New("node-1", []string{"node-2"}, nil, nil, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.role = Candidate
	r.currentTerm = 1
	r.mu.Unlock()

	err := r.Propose(context.Background(), []byte("SET foo bar"))
	assert.Error(t, err)
	var target ErrNotLeader
	assert.ErrorAs(t, err, &target)
}

func TestPropose_LeaderAppendsEntry(t *testing.T) {
	r := New("node-1", nil, nil, &recordingSM{}, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.role = Leader
	r.currentTerm = 3
	r.mu.Unlock()

	err := r.Propose(context.Background(), []byte("SET foo bar"))
	assert.NoError(t, err)

	r.mu.Lock()
	require.Len(t, r.log, 1)
	assert.Equal(t, 0, r.log[0].Index)
	assert.Equal(t, 3, r.log[0].Term)
	assert.Equal(t, "SET foo bar", string(r.log[0].Command))
	assert.Equal(t, 0, r.commitIndex)
	r.mu.Unlock()
}

func TestPropose_MultipleEntries(t *testing.T) {
	r := New("node-1", nil, nil, &recordingSM{}, nil, nil)
	r.electionTimer = time.NewTimer(time.Hour)
	r.mu.Lock()
	r.role = Leader
	r.currentTerm = 2
	r.mu.Unlock()

	err := r.Propose(context.Background(), []byte("first"))
	assert.NoError(t, err)

	err = r.Propose(context.Background(), []byte("second"))
	assert.NoError(t, err)

	r.mu.Lock()
	require.Len(t, r.log, 2)
	assert.Equal(t, "first", string(r.log[0].Command))
	assert.Equal(t, "second", string(r.log[1].Command))
	assert.Equal(t, 0, r.log[0].Index)
	assert.Equal(t, 1, r.log[1].Index)
	assert.Equal(t, 2, r.log[0].Term)
	assert.Equal(t, 2, r.log[1].Term)
	r.mu.Unlock()
}
