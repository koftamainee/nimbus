package raft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"quorum/internal/wal"
)

type NodeRole int

const (
	Follower NodeRole = iota
	PreCandidate
	Candidate
	Leader
)

type LogEntry struct {
	Index   int
	Term    int
	Command []byte
}

type ErrNotLeader struct {
	LeaderID string
}

func (e ErrNotLeader) Error() string {
	if e.LeaderID == "" {
		return "not leader"
	}
	return fmt.Sprintf("not leader; leader: %s", e.LeaderID)
}

var ErrLeadershipLost = errors.New("leadership lost")

type StateMachine interface {
	Apply(command []byte)
	Snapshot() ([]byte, error)
	Restore(data []byte) error
}

type Raft struct {
	mu sync.Mutex

	id    string
	peers []string

	currentTerm int
	votedFor    string
	log         []LogEntry

	commitIndex int
	lastApplied int

	lastIncludedIndex int
	lastIncludedTerm  int
	snapshot          []byte

	role NodeRole

	nextIndex  []int
	matchIndex []int

	leaderID string

	voteReqCh    chan VoteRequest
	voteRespCh   chan VoteResponse
	appendReqCh  chan AppendRequest
	appendRespCh chan AppendResponse

	electionTimer  *time.Timer
	heartbeatTimer *time.Timer

	transport    Transport
	stateMachine StateMachine
	snapshotter  Snapshotter
	pending      map[int]chan error
	wal          *wal.WAL
	logger       *slog.Logger

	ready chan struct{}
}

type Transport interface {
	RequestVote(peerID string, req VoteRequest) (VoteResponse, error)
	AppendEntries(peerID string, req AppendRequest) (AppendResponse, error)
	InstallSnapshot(peerID string, req InstallSnapshotRequest) (InstallSnapshotResponse, error)
}

type InstallSnapshotRequest struct {
	Term              int
	LeaderID          string
	LastIncludedIndex int
	LastIncludedTerm  int
	SnapshotData      []byte
}

type InstallSnapshotResponse struct {
	Term    int
	Success bool
}

type VoteRequest struct {
	Term         int
	CandidateID  string
	LastLogIndex int
	LastLogTerm  int
	PreVote      bool
}

type VoteResponse struct {
	Term        int
	VoteGranted bool
	PreVote     bool
}

type AppendRequest struct {
	Term         int
	LeaderID     string
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendResponse struct {
	Term    int
	Success bool

	ConflictTerm  int
	ConflictIndex int
}

func New(id string, peers []string, transport Transport, sm StateMachine, w *wal.WAL, logger *slog.Logger) *Raft {
	return newWithSnapshotter(id, peers, transport, sm, w, nil, logger)
}

func newWithSnapshotter(id string, peers []string, transport Transport, sm StateMachine, w *wal.WAL, snapshotter Snapshotter, logger *slog.Logger) *Raft {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	r := &Raft{
		id:                id,
		peers:             peers,
		role:              Follower,
		votedFor:          "",
		log:               make([]LogEntry, 0),
		transport:         transport,
		stateMachine:      sm,
		snapshotter:       snapshotter,
		lastApplied:       -1,
		lastIncludedIndex: -1,
		pending:           make(map[int]chan error),
		electionTimer:     nil,
		heartbeatTimer:    nil,
		voteReqCh:         make(chan VoteRequest),
		voteRespCh:        make(chan VoteResponse),
		appendReqCh:       make(chan AppendRequest),
		appendRespCh:      make(chan AppendResponse),
		ready:             make(chan struct{}),
		wal:               w,
		logger:            logger,
	}

	if snapshotter != nil {
		index, term, data, err := snapshotter.Load()
		if err == nil && data != nil {
			r.lastIncludedIndex = index
			r.lastIncludedTerm = term
			r.snapshot = data
			if sm != nil {
				sm.Restore(data)
			}
			r.infof("loaded snapshot", "lastIncludedIndex", index, "lastIncludedTerm", term)
		}
	}

	if w != nil {
		var count int
		w.Replay(func(data []byte) error {
			var entry LogEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				r.warnf("skipping corrupt WAL entry", "err", err)
				return nil
			}
			if entry.Index <= r.lastIncludedIndex {
				return nil
			}
			r.log = append(r.log, entry)
			count++
			return nil
		})

		if count > 0 {
			r.infof("replayed entries from WAL", "count", count)
		}

		for _, entry := range r.log {
			if sm != nil {
				sm.Apply(entry.Command)
			}
			if entry.Index > r.lastApplied {
				r.lastApplied = entry.Index
			}
		}
		if len(r.log) > 0 {
			r.commitIndex = r.lastLogIndex()
		}
	}

	return r
}

func (r *Raft) Run() {
	r.mu.Lock()
	r.electionTimer = time.NewTimer(0)
	r.heartbeatTimer = time.NewTimer(0)
	if !r.electionTimer.Stop() {
		select {
		case <-r.electionTimer.C:
		default:
		}
	}
	r.resetElectionTimer()
	<-r.heartbeatTimer.C
	r.mu.Unlock()
	r.infof("run started")
	close(r.ready)
	go r.applyLoop()
	for {
		select {
		case <-r.electionTimer.C:
			r.startElection()

		case <-r.heartbeatTimer.C:
			r.replicateLog()

		case req := <-r.voteReqCh:
			r.voteRespCh <- r.HandleRequestVote(req)

		case req := <-r.appendReqCh:
			r.appendRespCh <- r.HandleAppendEntries(req)
		}
	}
}

func (r *Raft) applyLoop() {
	for {
		time.Sleep(10 * time.Millisecond)
		r.applyCommitted()
	}
}

func (r *Raft) applyCommitted() {
	r.mu.Lock()

	for r.lastApplied < r.commitIndex {
		nextIdx := r.lastApplied + 1
		sliceIdx := r.logSlice(nextIdx)
		if sliceIdx < 0 || sliceIdx >= len(r.log) {
			break
		}
		r.lastApplied = nextIdx
		entry := r.log[sliceIdx]
		ch, ok := r.pending[nextIdx]
		delete(r.pending, nextIdx)
		r.mu.Unlock()

		if r.stateMachine != nil {
			r.stateMachine.Apply(entry.Command)
		}

		r.infof("applied entry", "index", r.lastApplied, "command", string(entry.Command))

		if ok {
			ch <- nil
		}

		r.mu.Lock()
	}

	r.mu.Unlock()
}

func (r *Raft) clearPending() {
	n := len(r.pending)
	for idx, ch := range r.pending {
		delete(r.pending, idx)
		ch <- ErrLeadershipLost
	}
	if n > 0 {
		r.warnf("leadership lost", "dropped", n)
	}
}

func (r *Raft) HandleRequestVote(req VoteRequest) VoteResponse {
	r.mu.Lock()
	defer r.mu.Unlock()

	if req.PreVote {
		return r.handlePreVote(req)
	}

	if req.Term < r.currentTerm {
		return VoteResponse{
			Term:        r.currentTerm,
			VoteGranted: false,
		}
	}

	if req.Term > r.currentTerm {
		wasLeader := r.role == Leader
		r.currentTerm = req.Term
		r.votedFor = ""
		r.role = Follower
		r.leaderID = ""
		if wasLeader {
			r.clearPending()
		}
		r.resetElectionTimer()
		r.infof("stepping down", "term", req.Term, "from", req.CandidateID)
	}

	if r.votedFor != "" && r.votedFor != req.CandidateID {
		return VoteResponse{
			Term:        r.currentTerm,
			VoteGranted: false,
		}
	}

	lastIdx := r.lastLogIndex()
	lastTerm := r.lastLogTerm()
	if req.LastLogTerm < lastTerm ||
		(req.LastLogTerm == lastTerm && req.LastLogIndex < lastIdx) {
		return VoteResponse{Term: r.currentTerm, VoteGranted: false}
	}
	r.votedFor = req.CandidateID
	r.resetElectionTimer()
	return VoteResponse{
		Term:        r.currentTerm,
		VoteGranted: true,
	}
}

func (r *Raft) handlePreVote(req VoteRequest) VoteResponse {
	lastIdx := r.lastLogIndex()
	lastTerm := r.lastLogTerm()

	if req.LastLogTerm < lastTerm ||
		(req.LastLogTerm == lastTerm && req.LastLogIndex < lastIdx) {
		return VoteResponse{Term: r.currentTerm, VoteGranted: false, PreVote: true}
	}

	return VoteResponse{Term: r.currentTerm, VoteGranted: true, PreVote: true}
}

func (r *Raft) HandleAppendEntries(req AppendRequest) AppendResponse {
	r.mu.Lock()
	defer r.mu.Unlock()

	if req.Term < r.currentTerm {
		return AppendResponse{
			Term:    r.currentTerm,
			Success: false,
		}
	}

	if req.Term > r.currentTerm {
		wasLeader := r.role == Leader
		r.currentTerm = req.Term
		r.votedFor = ""
		r.role = Follower
		r.leaderID = ""
		if wasLeader {
			r.clearPending()
		}
		r.infof("stepping down", "term", req.Term, "from", req.LeaderID)
	}

	r.leaderID = req.LeaderID

	if req.PrevLogIndex >= 0 {
		if req.PrevLogIndex > r.lastLogIndex() {
			return AppendResponse{Term: r.currentTerm, Success: false}
		}
		if r.getTerm(req.PrevLogIndex) != req.PrevLogTerm {
			return AppendResponse{Term: r.currentTerm, Success: false}
		}
	}

	for i, entry := range req.Entries {
		idx := req.PrevLogIndex + 1 + i
		if idx <= r.lastLogIndex() {
			if r.getTerm(idx) != entry.Term {
				sliceIdx := r.logSlice(idx)
				if sliceIdx >= 0 && sliceIdx < len(r.log) {
					r.log = r.log[:sliceIdx]
				}
				r.appendEntry(entry)
			}
		} else {
			r.appendEntry(entry)
		}
	}

	if req.LeaderCommit > r.commitIndex {
		r.commitIndex = min(req.LeaderCommit, r.lastLogIndex())
	}

	r.resetElectionTimer()

	if len(req.Entries) > 0 {
		r.infof("appended entries", "from", req.LeaderID, "count", len(req.Entries))
	}

	return AppendResponse{Term: r.currentTerm, Success: true}
}

func (r *Raft) HandleInstallSnapshot(req InstallSnapshotRequest) InstallSnapshotResponse {
	r.mu.Lock()
	defer r.mu.Unlock()

	if req.Term < r.currentTerm {
		return InstallSnapshotResponse{Term: r.currentTerm, Success: false}
	}

	if req.Term > r.currentTerm {
		wasLeader := r.role == Leader
		r.currentTerm = req.Term
		r.votedFor = ""
		r.role = Follower
		r.leaderID = ""
		if wasLeader {
			r.clearPending()
		}
		r.infof("stepping down during InstallSnapshot", "term", req.Term)
	}

	if req.LastIncludedIndex <= r.lastIncludedIndex {
		r.infof("ignoring old snapshot", "lastIncluded", r.lastIncludedIndex, "requested", req.LastIncludedIndex)
		return InstallSnapshotResponse{Term: r.currentTerm, Success: true}
	}

	if r.stateMachine != nil {
		if err := r.stateMachine.Restore(req.SnapshotData); err != nil {
			r.warnf("failed to restore state machine from snapshot", "err", err)
			return InstallSnapshotResponse{Term: r.currentTerm, Success: false}
		}
	}

	r.snapshot = req.SnapshotData
	r.lastIncludedIndex = req.LastIncludedIndex
	r.lastIncludedTerm = req.LastIncludedTerm

	var newLog []LogEntry
	for _, entry := range r.log {
		if entry.Index > r.lastIncludedIndex {
			newLog = append(newLog, entry)
		}
	}
	r.log = newLog

	if r.lastApplied < r.lastIncludedIndex {
		r.lastApplied = r.lastIncludedIndex
	}
	if r.commitIndex < r.lastIncludedIndex {
		r.commitIndex = r.lastIncludedIndex
	}

	if r.snapshotter != nil {
		r.snapshotter.Save(r.lastIncludedIndex, r.lastIncludedTerm, r.snapshot)
	}

	if r.wal != nil {
		r.wal.Truncate(func(data []byte) bool {
			var entry LogEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				return false
			}
			return entry.Index > r.lastIncludedIndex
		})
	}

	r.infof("snapshot installed", "lastIncludedIndex", r.lastIncludedIndex, "lastIncludedTerm", r.lastIncludedTerm)

	return InstallSnapshotResponse{Term: r.currentTerm, Success: true}
}

func (r *Raft) Ready() <-chan struct{} {
	return r.ready
}

func (r *Raft) TakeSnapshot() error {
	r.mu.Lock()

	if r.stateMachine == nil || r.snapshotter == nil {
		r.mu.Unlock()
		return nil
	}

	index := r.lastApplied
	term := r.getTerm(index)
	r.mu.Unlock()

	data, err := r.stateMachine.Snapshot()
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if index < r.lastIncludedIndex {
		return nil
	}

	r.snapshot = data
	r.lastIncludedIndex = index
	r.lastIncludedTerm = term

	var newLog []LogEntry
	for _, entry := range r.log {
		if entry.Index > index {
			newLog = append(newLog, entry)
		}
	}
	r.log = newLog

	if r.wal != nil {
		r.wal.Truncate(func(data []byte) bool {
			var entry LogEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				return false
			}
			return entry.Index > index
		})
	}

	if r.snapshotter != nil {
		r.snapshotter.Save(index, term, data)
	}

	r.infof("snapshot taken", "index", index, "term", term, "remainingLogEntries", len(r.log))
	return nil
}

func (r *Raft) SnapshotStatus() (index, term int, exists bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastIncludedIndex < 0 {
		return 0, 0, false
	}
	return r.lastIncludedIndex, r.lastIncludedTerm, true
}

func (r *Raft) SetStateMachine(sm StateMachine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stateMachine = sm
}

func (r *Raft) SetSnapshotter(s Snapshotter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshotter = s

	index, term, data, err := s.Load()
	if err != nil || data == nil {
		return
	}

	if index <= r.lastIncludedIndex {
		return
	}

	if r.stateMachine != nil {
		r.stateMachine.Restore(data)
	}

	r.snapshot = data
	r.lastIncludedIndex = index
	r.lastIncludedTerm = term

	var newLog []LogEntry
	for _, entry := range r.log {
		if entry.Index > index {
			newLog = append(newLog, entry)
		}
	}
	r.log = newLog

	if r.lastApplied < index {
		r.lastApplied = index
	}
	if r.commitIndex < index {
		r.commitIndex = index
	}

	r.infof("loaded snapshot via SetSnapshotter", "lastIncludedIndex", index, "lastIncludedTerm", term)
}

func (r *Raft) Propose(ctx context.Context, command []byte) error {
	r.mu.Lock()

	if r.role != Leader {
		leaderID := r.leaderID
		r.mu.Unlock()
		return ErrNotLeader{LeaderID: leaderID}
	}

	idx := r.lastLogIndex() + 1
	ch := make(chan error, 1)
	r.pending[idx] = ch

	r.appendEntry(LogEntry{
		Index:   idx,
		Term:    r.currentTerm,
		Command: command,
	})

	r.infof("proposed", "command", string(command), "index", idx)

	if len(r.peers) == 0 {
		r.commitIndex = idx
		delete(r.pending, idx)
		r.mu.Unlock()
		r.applyCommitted()
		close(ch)
		return nil
	}
	r.mu.Unlock()

	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
	}

	r.mu.Lock()
	if _, ok := r.pending[idx]; ok {
		delete(r.pending, idx)
		r.mu.Unlock()
		return ctx.Err()
	}
	r.mu.Unlock()
	return nil
}

func (r *Raft) ReadIndex(ctx context.Context) error {
	r.mu.Lock()
	if r.role != Leader {
		leaderID := r.leaderID
		r.mu.Unlock()
		return ErrNotLeader{LeaderID: leaderID}
	}
	readIdx := r.commitIndex
	term := r.currentTerm
	r.mu.Unlock()

	if len(r.peers) == 0 {
		return nil
	}

	if err := r.heartbeatQuorum(ctx, term); err != nil {
		return err
	}

	for {
		r.mu.Lock()
		if r.commitIndex >= readIdx || r.role != Leader {
			isLeader := r.role == Leader
			leaderID := r.leaderID
			r.mu.Unlock()
			if !isLeader {
				return ErrNotLeader{LeaderID: leaderID}
			}
			return nil
		}
		r.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (r *Raft) heartbeatQuorum(ctx context.Context, term int) error {
	need := len(r.peers)/2 + 1
	votes := make(chan struct{}, len(r.peers))

	for i, peer := range r.peers {
		p := peer
		idx := i
		go func() {
			r.mu.Lock()
			next := r.nextIndex[idx]
			prevLogIndex := next - 1
			prevLogTerm := r.getTerm(prevLogIndex)
			leaderCommit := r.commitIndex
			r.mu.Unlock()

			req := AppendRequest{
				Term:         term,
				LeaderID:     r.id,
				PrevLogIndex: prevLogIndex,
				PrevLogTerm:  prevLogTerm,
				LeaderCommit: leaderCommit,
			}

			resp, err := r.transport.AppendEntries(p, req)
			if err != nil {
				return
			}

			r.mu.Lock()
			if resp.Term > r.currentTerm {
				r.currentTerm = resp.Term
				r.votedFor = ""
				r.role = Follower
				r.leaderID = ""
				r.clearPending()
				r.resetElectionTimer()
				r.infof("stepping down", "term", resp.Term, "from", p)
				r.mu.Unlock()
				return
			}
			r.mu.Unlock()

			if resp.Success {
				votes <- struct{}{}
			}
		}()
	}

	for i := 0; i < need-1; i++ {
		select {
		case <-votes:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	r.mu.Lock()
	stillLeader := r.role == Leader && r.currentTerm == term
	r.mu.Unlock()
	if !stillLeader {
		return ErrNotLeader{}
	}
	return nil
}

func (r *Raft) startElection() {
	r.mu.Lock()

	term := r.currentTerm
	lastLogIndex := r.lastLogIndex()
	lastLogTerm := r.lastLogTerm()
	r.role = PreCandidate
	r.mu.Unlock()

	if r.transport != nil && !r.runPreVote(term, lastLogIndex, lastLogTerm) {
		r.mu.Lock()
		r.role = Follower
		r.resetElectionTimer()
		r.mu.Unlock()
		return
	}

	r.mu.Lock()
	if r.role != PreCandidate {
		r.mu.Unlock()
		return
	}
	r.role = Candidate
	r.currentTerm++
	r.votedFor = r.id
	r.leaderID = ""
	r.clearPending()

	term = r.currentTerm
	lastLogIndex = r.lastLogIndex()
	lastLogTerm = r.lastLogTerm()
	r.infof("election started", "term", term)

	r.resetElectionTimer()
	r.mu.Unlock()

	votes := 1
	for _, peer := range r.peers {
		p := peer
		go func() {
			if r.transport == nil {
				return
			}

			resp, err := r.transport.RequestVote(p, VoteRequest{
				Term:         term,
				CandidateID:  r.id,
				LastLogIndex: lastLogIndex,
				LastLogTerm:  lastLogTerm,
			})
			if err != nil {
				return
			}

			r.mu.Lock()
			if resp.Term > r.currentTerm {
				r.currentTerm = resp.Term
				r.votedFor = ""
				r.role = Follower
				r.leaderID = ""
				r.resetElectionTimer()
				r.mu.Unlock()
				return
			}
			if !resp.VoteGranted {
				r.mu.Unlock()
				return
			}

			votes++
			r.infof("received vote", "from", p, "term", term)
			if votes > len(r.peers)/2 && r.role == Candidate && r.currentTerm == term {
				r.becomeLeader()
			}
			r.mu.Unlock()
		}()
	}

	if len(r.peers) == 0 {
		r.mu.Lock()
		r.becomeLeader()
		r.mu.Unlock()
	}
}

func (r *Raft) runPreVote(term, lastLogIndex, lastLogTerm int) bool {
	type preVoteResult struct {
		peer  string
		grant bool
	}
	results := make(chan preVoteResult, len(r.peers))

	for _, peer := range r.peers {
		p := peer
		go func() {
			if r.transport == nil {
				results <- preVoteResult{p, false}
				return
			}

			resp, err := r.transport.RequestVote(p, VoteRequest{
				Term:         term,
				CandidateID:  r.id,
				LastLogIndex: lastLogIndex,
				LastLogTerm:  lastLogTerm,
				PreVote:      true,
			})
			if err != nil {
				results <- preVoteResult{p, false}
				return
			}

			results <- preVoteResult{p, resp.VoteGranted}
		}()
	}

	grants := 1
	need := len(r.peers)/2 + 1
	received := 0

	for received < len(r.peers) {
		res := <-results
		received++
		if res.grant {
			grants++
			if grants >= need {
				go func() {
					for ; received < len(r.peers); received++ {
						<-results
					}
				}()
				return true
			}
		}
	}

	return false
}

func (r *Raft) becomeLeader() {
	r.role = Leader
	r.leaderID = r.id
	r.resetHeartbeatTimer()
	if !r.electionTimer.Stop() {
		select {
		case <-r.electionTimer.C:
		default:
		}
	}
	r.nextIndex = make([]int, len(r.peers))
	r.matchIndex = make([]int, len(r.peers))
	for i := range r.peers {
		r.nextIndex[i] = r.lastLogIndex() + 1
	}
	r.infof("became leader", "term", r.currentTerm)
}

func (r *Raft) replicateLog() {
	r.mu.Lock()
	if r.role != Leader {
		r.mu.Unlock()
		return
	}

	term := r.currentTerm
	peers := make([]string, len(r.peers))
	copy(peers, r.peers)
	r.resetHeartbeatTimer()
	r.mu.Unlock()

	for i, peer := range peers {
		p := peer
		idx := i
		go func() {
			if r.transport == nil {
				return
			}

			r.mu.Lock()
			next := r.nextIndex[idx]

			if next <= r.lastIncludedIndex {
				r.mu.Unlock()
				r.sendInstallSnapshot(p, idx, term)
				return
			}

			prevLogIndex := next - 1
			prevLogTerm := r.getTerm(prevLogIndex)

			var entries []LogEntry
			sliceStart := r.logSlice(next)
			if sliceStart >= 0 && sliceStart < len(r.log) {
				entries = make([]LogEntry, len(r.log)-sliceStart)
				copy(entries, r.log[sliceStart:])
			}

			leaderCommit := r.commitIndex
			r.mu.Unlock()

			req := AppendRequest{
				Term:         term,
				LeaderID:     r.id,
				PrevLogIndex: prevLogIndex,
				PrevLogTerm:  prevLogTerm,
				Entries:      entries,
				LeaderCommit: leaderCommit,
			}

			resp, err := r.transport.AppendEntries(p, req)
			if err != nil {
				return
			}

			r.mu.Lock()
			defer r.mu.Unlock()

			if len(req.Entries) > 0 {
				if resp.Success {
					r.infof("replicated", "to", p, "entries", len(req.Entries))
				} else {
					r.warnf("replicate conflict", "peer", p, "nextIndex", fmt.Sprintf("%d→%d", next, max(r.lastIncludedIndex+1, next-1)))
				}
			}

			if resp.Term > r.currentTerm {
				r.currentTerm = resp.Term
				r.votedFor = ""
				r.role = Follower
				r.leaderID = ""
				r.clearPending()
				r.resetElectionTimer()
				r.infof("stepping down", "term", resp.Term, "from", p)
				return
			}

			if !resp.Success {
				r.nextIndex[idx] = max(r.lastIncludedIndex+1, next-1)
				return
			}

			if len(entries) > 0 {
				r.matchIndex[idx] = next + len(entries) - 1
				r.nextIndex[idx] = r.matchIndex[idx] + 1
			}

			for n := r.lastLogIndex(); n > r.commitIndex; n-- {
				if r.getTerm(n) != r.currentTerm {
					continue
				}
				count := 1
				for j := range r.peers {
					if r.matchIndex[j] >= n {
						count++
					}
				}
				if count > len(r.peers)/2 {
					if n > r.commitIndex {
						r.infof("commitIndex advanced", "index", n, "term", r.getTerm(n))
					}
					r.commitIndex = n
					break
				}
			}
		}()
	}
}

func (r *Raft) sendInstallSnapshot(peerID string, peerIdx, term int) {
	r.mu.Lock()
	if r.snapshot == nil {
		r.mu.Unlock()
		return
	}
	req := InstallSnapshotRequest{
		Term:              r.currentTerm,
		LeaderID:          r.id,
		LastIncludedIndex: r.lastIncludedIndex,
		LastIncludedTerm:  r.lastIncludedTerm,
		SnapshotData:      r.snapshot,
	}
	r.mu.Unlock()

	resp, err := r.transport.InstallSnapshot(peerID, req)
	if err != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if resp.Term > r.currentTerm {
		r.currentTerm = resp.Term
		r.votedFor = ""
		r.role = Follower
		r.leaderID = ""
		r.clearPending()
		r.resetElectionTimer()
		r.infof("stepping down during InstallSnapshot", "term", resp.Term, "from", peerID)
		return
	}

	if resp.Success {
		r.nextIndex[peerIdx] = r.lastIncludedIndex + 1
		r.matchIndex[peerIdx] = r.lastIncludedIndex
		r.infof("installed snapshot", "peer", peerID, "lastIncluded", r.lastIncludedIndex)
	}
}

func (r *Raft) appendEntry(entry LogEntry) {
	r.log = append(r.log, entry)
	if r.wal != nil {
		data, err := json.Marshal(entry)
		if err != nil {
			r.warnf("failed to marshal log entry", "err", err)
			return
		}
		if err := r.wal.Append(data); err != nil {
			r.warnf("failed to append log entry to WAL", "err", err)
		}
	}
}

func (r *Raft) resetElectionTimer() {
	if !r.electionTimer.Stop() {
		select {
		case <-r.electionTimer.C:
		default:
		}
	}
	r.electionTimer.Reset(randomElectionTimeout())
}

func (r *Raft) resetHeartbeatTimer() {
	if !r.heartbeatTimer.Stop() {
		select {
		case <-r.heartbeatTimer.C:
		default:
		}
	}
	r.heartbeatTimer.Reset(50 * time.Millisecond)
}

func (r *Raft) lastLogIndex() int {
	return r.lastIncludedIndex + len(r.log)
}

func (r *Raft) lastLogTerm() int {
	if len(r.log) == 0 {
		return r.lastIncludedTerm
	}
	return r.log[len(r.log)-1].Term
}

func (r *Raft) getTerm(index int) int {
	if index == r.lastIncludedIndex {
		return r.lastIncludedTerm
	}
	if index < r.lastIncludedIndex || index > r.lastLogIndex() {
		return 0
	}
	return r.log[index-r.lastIncludedIndex-1].Term
}

func (r *Raft) logSlice(index int) int {
	return index - r.lastIncludedIndex - 1
}

func randomElectionTimeout() time.Duration {
	return time.Duration(150+rand.Intn(150)) * time.Millisecond
}
