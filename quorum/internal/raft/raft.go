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
	pending      map[int]chan error
	wal          *wal.WAL
	logger       *slog.Logger

	ready chan struct{}
}

type Transport interface {
	RequestVote(peerID string, req VoteRequest) (VoteResponse, error)
	AppendEntries(peerID string, req AppendRequest) (AppendResponse, error)
}

type VoteRequest struct {
	Term         int
	CandidateID  string
	LastLogIndex int
	LastLogTerm  int
}

type VoteResponse struct {
	Term        int
	VoteGranted bool
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
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	r := &Raft{
		id:             id,
		peers:          peers,
		role:           Follower,
		votedFor:       "",
		log:            make([]LogEntry, 0),
		transport:      transport,
		stateMachine:   sm,
		lastApplied:    -1,
		pending:        make(map[int]chan error),
		electionTimer:  nil,
		heartbeatTimer: nil,
		voteReqCh:      make(chan VoteRequest),
		voteRespCh:     make(chan VoteResponse),
		appendReqCh:    make(chan AppendRequest),
		appendRespCh:   make(chan AppendResponse),
		ready:          make(chan struct{}),
		wal:            w,
		logger:         logger,
	}

	if w != nil {
		var count int
		w.Replay(func(data []byte) error {
			var entry LogEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				r.warnf("skipping corrupt WAL entry", "err", err)
				return nil
			}
			r.log = append(r.log, entry)
			count++
			return nil
		})

		if count > 0 {
			r.infof("replayed entries from WAL", "count", count)
		}

		if len(r.log) > 0 {
			for _, entry := range r.log {
				if sm != nil {
					sm.Apply(entry.Command)
				}
			}
			r.lastApplied = len(r.log) - 1
			r.commitIndex = len(r.log) - 1
		}
	}

	return r
}

func (r *Raft) Run() {
	r.mu.Lock()
	r.electionTimer = time.NewTimer(0)
	r.heartbeatTimer = time.NewTimer(0)
	// election timer starts now
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

	for r.lastApplied < r.commitIndex && r.lastApplied+1 < len(r.log) {
		r.lastApplied++
		entry := r.log[r.lastApplied]
		ch, ok := r.pending[r.lastApplied]
		delete(r.pending, r.lastApplied)
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

// Must be called with r.mu held.
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
			r.resetElectionTimer()
		}
		r.infof("stepping down", "term", req.Term, "from", req.CandidateID)
	}

	if r.votedFor != "" && r.votedFor != req.CandidateID {
		return VoteResponse{
			Term:        r.currentTerm,
			VoteGranted: false,
		}
	}

	lastIdx := len(r.log) - 1
	lastTerm := 0
	if lastIdx >= 0 {
		lastTerm = r.log[lastIdx].Term
	}
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
		if req.PrevLogIndex >= len(r.log) {
			return AppendResponse{Term: r.currentTerm, Success: false}
		}
		if r.log[req.PrevLogIndex].Term != req.PrevLogTerm {
			return AppendResponse{Term: r.currentTerm, Success: false}
		}
	}

	for i, entry := range req.Entries {
		idx := req.PrevLogIndex + 1 + i
		if idx < len(r.log) {
			if r.log[idx].Term != entry.Term {
				r.log = r.log[:idx]
				r.appendEntry(entry)
			}
		} else {
			r.appendEntry(entry)
		}
	}

	if req.LeaderCommit > r.commitIndex {
		r.commitIndex = min(req.LeaderCommit, len(r.log)-1)
	}

	r.resetElectionTimer()

	if len(req.Entries) > 0 {
		r.infof("appended entries", "from", req.LeaderID, "count", len(req.Entries))
	}

	return AppendResponse{Term: r.currentTerm, Success: true}
}

func (r *Raft) Ready() <-chan struct{} {
	return r.ready
}

func (r *Raft) SetStateMachine(sm StateMachine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stateMachine = sm
}

func (r *Raft) Propose(ctx context.Context, command []byte) error {
	r.mu.Lock()

	if r.role != Leader {
		leaderID := r.leaderID
		r.mu.Unlock()
		return ErrNotLeader{LeaderID: leaderID}
	}

	idx := len(r.log)
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

// ReadIndex проверяет что нода всё ещё лидер и все коммиты видны.
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

	// single node: always quorate
	if len(r.peers) == 0 {
		return nil
	}

	if err := r.heartbeatQuorum(ctx, term); err != nil {
		return err
	}

	// ждём пока commitIndex догонит readIdx
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

// heartbeatQuorum отправляет heartbeat всем пирам и ждёт кворум.
func (r *Raft) heartbeatQuorum(ctx context.Context, term int) error {
	need := len(r.peers)/2 + 1 // quorum включая себя
	votes := make(chan struct{}, len(r.peers))

	for i, peer := range r.peers {
		p := peer
		idx := i
		go func() {
			r.mu.Lock()
			next := r.nextIndex[idx]
			prevLogIndex := next - 1
			prevLogTerm := 0
			if prevLogIndex >= 0 && prevLogIndex < len(r.log) {
				prevLogTerm = r.log[prevLogIndex].Term
			}
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

	// ждём голоса от need-1 пиров (себя считаем)
	for i := 0; i < need-1; i++ {
		select {
		case <-votes:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// финальная проверка: не упали ли мы за время сбора голосов
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

	wasLeader := r.role == Leader
	r.role = Candidate
	r.currentTerm++
	r.votedFor = r.id
	r.leaderID = ""
	if wasLeader {
		r.clearPending()
	}

	term := r.currentTerm
	lastLogIndex := len(r.log) - 1
	lastLogTerm := 0
	if lastLogIndex >= 0 {
		lastLogTerm = r.log[lastLogIndex].Term
	}
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
			if err != nil || !resp.VoteGranted {
				return
			}

			r.mu.Lock()
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

// NOTE: Should be called with already locked mutex
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
		r.nextIndex[i] = len(r.log)
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
			prevLogIndex := next - 1
			prevLogTerm := 0
			if prevLogIndex >= 0 && prevLogIndex < len(r.log) {
				prevLogTerm = r.log[prevLogIndex].Term
			}
			var entries []LogEntry
			if next < len(r.log) {
				entries = make([]LogEntry, len(r.log)-next)
				copy(entries, r.log[next:])
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
					r.warnf("replicate conflict", "peer", p, "nextIndex", fmt.Sprintf("%d→%d", next, max(1, next-1)))
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
				r.nextIndex[idx] = max(1, next-1)
				return
			}

			if len(entries) > 0 {
				r.matchIndex[idx] = next + len(entries) - 1
				r.nextIndex[idx] = r.matchIndex[idx] + 1
			}

			for n := len(r.log) - 1; n > r.commitIndex; n-- {
				if r.log[n].Term != r.currentTerm {
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
						r.infof("commitIndex advanced", "index", n, "term", r.log[n].Term)
					}
					r.commitIndex = n
					break
				}
			}
		}()
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

func randomElectionTimeout() time.Duration {
	return time.Duration(150+rand.Intn(150)) * time.Millisecond
}
