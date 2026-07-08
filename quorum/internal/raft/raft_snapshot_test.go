package raft

import (
	"context"
	"testing"
	"time"
)

func TestSnapshotterSaveLoad(t *testing.T) {
	dir := t.TempDir()
	s := NewFileSnapshotter(dir)

	err := s.Save(10, 1, []byte("hello"))
	if err != nil {
		t.Fatalf("Save failed: %s", err)
	}

	index, term, data, err := s.Load()
	if err != nil {
		t.Fatalf("Load failed: %s", err)
	}
	if index != 10 || term != 1 || string(data) != "hello" {
		t.Fatalf("got index=%d term=%d data=%s", index, term, data)
	}

	err = s.Save(20, 2, []byte("world"))
	if err != nil {
		t.Fatalf("Save failed: %s", err)
	}

	index, term, data, err = s.Load()
	if err != nil {
		t.Fatalf("Load failed: %s", err)
	}
	if index != 20 || term != 2 || string(data) != "world" {
		t.Fatalf("got index=%d term=%d data=%s", index, term, data)
	}

	files, err := s.list()
	if err != nil {
		t.Fatalf("list failed: %s", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestSnapshotterLoadEmpty(t *testing.T) {
	dir := t.TempDir()
	s := NewFileSnapshotter(dir)

	index, term, data, err := s.Load()
	if err != nil {
		t.Fatalf("Load failed: %s", err)
	}
	if data != nil {
		t.Fatalf("expected nil data, got %s", data)
	}
	if index != 0 || term != 0 {
		t.Fatalf("expected index=0 term=0, got %d %d", index, term)
	}
}

func TestTakeSnapshot(t *testing.T) {
	nodes, _ := newCluster(t, []string{"n1", "n2", "n3"})
	defer func() {
		for _, n := range nodes {
			n.mu.Lock()
			n.role = Follower
			n.mu.Unlock()
		}
	}()

	time.Sleep(2 * time.Second)

	var leader *Raft
	for _, n := range nodes {
		n.mu.Lock()
		if n.role == Leader {
			leader = n
		}
		n.mu.Unlock()
	}
	if leader == nil {
		t.Fatal("no leader elected")
	}

	sm := &recordingSM{}
	leader.SetStateMachine(sm)

	dir := t.TempDir()
	snap := NewFileSnapshotter(dir)
	leader.SetSnapshotter(snap)

	for i := 0; i < 5; i++ {
		err := leader.Propose(context.Background(), []byte("cmd"))
		if err != nil {
			t.Fatalf("Propose failed: %s", err)
		}
	}

	time.Sleep(200 * time.Millisecond)
	leader.applyCommitted()

	if err := leader.TakeSnapshot(); err != nil {
		t.Fatalf("TakeSnapshot failed: %s", err)
	}

	_, _, exists := leader.SnapshotStatus()
	if !exists {
		t.Fatal("snapshot should exist")
	}

	if len(leader.log) != 0 {
		t.Fatalf("expected empty log after snapshot, got %d entries", len(leader.log))
	}
}

func TestInstallSnapshot(t *testing.T) {
	f1 := New("f1", nil, nil, &recordingSM{}, nil, nil)
	f1.electionTimer = time.NewTimer(time.Hour)
	f1.mu.Lock()
	f1.currentTerm = 1
	f1.role = Follower
	f1.lastApplied = 10
	f1.mu.Unlock()

	sm := &recordingSM{}
	f1.SetStateMachine(sm)
	dir := t.TempDir()
	snap := NewFileSnapshotter(dir)
	f1.SetSnapshotter(snap)

	resp := f1.HandleInstallSnapshot(InstallSnapshotRequest{
		Term:              1,
		LeaderID:          "leader",
		LastIncludedIndex: 10,
		LastIncludedTerm:  1,
		SnapshotData:      []byte("snapshot-data"),
	})

	if !resp.Success {
		t.Fatal("InstallSnapshot should succeed")
	}

	if f1.lastIncludedIndex != 10 {
		t.Fatalf("expected lastIncludedIndex=10, got %d", f1.lastIncludedIndex)
	}

	index, term, _, err := snap.Load()
	if err != nil {
		t.Fatalf("Load failed: %s", err)
	}
	if index != 10 || term != 1 {
		t.Fatalf("expected index=10 term=1, got index=%d term=%d", index, term)
	}
}

func TestRejectOldSnapshot(t *testing.T) {
	f1 := New("f1", nil, nil, &recordingSM{}, nil, nil)
	f1.electionTimer = time.NewTimer(time.Hour)
	f1.mu.Lock()
	f1.lastIncludedIndex = 20
	f1.currentTerm = 1
	f1.role = Follower
	f1.mu.Unlock()

	resp := f1.HandleInstallSnapshot(InstallSnapshotRequest{
		Term:              1,
		LeaderID:          "leader",
		LastIncludedIndex: 10,
		LastIncludedTerm:  1,
		SnapshotData:      []byte("old"),
	})

	if !resp.Success {
		t.Fatal("should succeed (old snapshot is not an error)")
	}

	if f1.lastIncludedIndex != 20 {
		t.Fatalf("should keep lastIncludedIndex=20, got %d", f1.lastIncludedIndex)
	}
}

func TestRejectInstallSnapshotLowerTerm(t *testing.T) {
	f1 := New("f1", nil, nil, &recordingSM{}, nil, nil)
	f1.electionTimer = time.NewTimer(time.Hour)
	f1.mu.Lock()
	f1.currentTerm = 5
	f1.role = Follower
	f1.mu.Unlock()

	resp := f1.HandleInstallSnapshot(InstallSnapshotRequest{
		Term:              3,
		LeaderID:          "leader",
		LastIncludedIndex: 10,
		LastIncludedTerm:  3,
		SnapshotData:      []byte("old-term"),
	})

	if resp.Success {
		t.Fatal("should reject snapshot with lower term")
	}
	if resp.Term != 5 {
		t.Fatalf("should return current term 5, got %d", resp.Term)
	}
}
