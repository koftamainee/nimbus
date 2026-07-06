package raft

import (
	"math/rand"
	"sync"
	"time"
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

	voteReqCh    chan VoteRequest
	voteRespCh   chan VoteResponse
	appendReqCh  chan AppendRequest
	appendRespCh chan AppendResponse

	electionTimer  *time.Timer
	heartbeatTimer *time.Timer

	transport Transport

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

func New(id string, peers []string, transport Transport) *Raft {
	return &Raft{
		id:             id,
		peers:          peers,
		role:           Follower,
		votedFor:       "",
		log:            make([]LogEntry, 0),
		transport:      transport,
		electionTimer:  nil,
		heartbeatTimer: nil,
		voteReqCh:      make(chan VoteRequest),
		voteRespCh:     make(chan VoteResponse),
		appendReqCh:    make(chan AppendRequest),
		appendRespCh:   make(chan AppendResponse),
		ready:          make(chan struct{}),
	}
}

func (r *Raft) Run() {
	r.mu.Lock()
	r.electionTimer = time.NewTimer(randomElectionTimeout())
	r.heartbeatTimer = time.NewTimer(0)
	<-r.heartbeatTimer.C
	r.mu.Unlock()
	close(r.ready)
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
		r.currentTerm = req.Term
		r.votedFor = ""
		r.role = Follower
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
	r.electionTimer.Reset(randomElectionTimeout())
	return VoteResponse{
		Term:        r.currentTerm,
		VoteGranted: true,
	}
}

func (r *Raft) HandleAppendEntries(req AppendRequest) AppendResponse {
	return AppendResponse{}
}

func (r *Raft) Propose(command []byte) (bool, error) {
	return false, nil
}

func (r *Raft) startElection() {
	r.mu.Lock()

	r.role = Candidate
	r.currentTerm++
	r.votedFor = r.id

	term := r.currentTerm
	lastLogIndex := len(r.log) - 1
	lastLogTerm := 0
	if lastLogIndex >= 0 {
		lastLogTerm = r.log[lastLogIndex].Term
	}
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
			if votes > len(r.peers)/2 {
				r.becomeLeader()
			}
			r.mu.Unlock()
		}()
	}
}

// NOTE: Should be called with already locked mutex
func (r *Raft) becomeLeader() {
	r.role = Leader
	r.heartbeatTimer.Reset(50 * time.Millisecond)
	r.nextIndex = make([]int, len(r.peers))
	r.matchIndex = make([]int, len(r.peers))
	for i := range r.peers {
		r.nextIndex[i] = len(r.log)
	}

}

func (r *Raft) replicateLog() {}

func (r *Raft) commitUpToIndex(index int) {}

func (r *Raft) save() {}
func (r *Raft) load() {}

func randomElectionTimeout() time.Duration {
	return time.Duration(150+rand.Intn(150)) * time.Millisecond
}
