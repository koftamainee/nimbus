package raft

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"

	quorumv1 "quorum/gen/quorum/v1"
)

func grpcDialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  50 * time.Millisecond,
				Multiplier: 1.5,
				Jitter:     0.2,
				MaxDelay:   3 * time.Second,
			},
		}),
	}
}

type GRPCTransport struct {
	mu    sync.Mutex
	peers map[string]string
	conns map[string]*grpc.ClientConn
}

func NewGRPCTransport() *GRPCTransport {
	return &GRPCTransport{
		peers: make(map[string]string),
		conns: make(map[string]*grpc.ClientConn),
	}
}

func (t *GRPCTransport) SetPeer(id, addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.peers[id] = addr
}

func (t *GRPCTransport) RemovePeer(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.peers, id)
	if c, ok := t.conns[id]; ok {
		c.Close()
		delete(t.conns, id)
	}
}

func (t *GRPCTransport) getConn(peerID string) (*grpc.ClientConn, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if c, ok := t.conns[peerID]; ok {
		return c, nil
	}

	addr, ok := t.peers[peerID]
	if !ok {
		return nil, nil
	}

	conn, err := grpc.NewClient(addr, grpcDialOptions()...)
	if err != nil {
		return nil, err
	}
	t.conns[peerID] = conn
	return conn, nil
}

func rpcContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 500*time.Millisecond)
}

func (t *GRPCTransport) RequestVote(peerID string, req VoteRequest) (VoteResponse, error) {
	conn, err := t.getConn(peerID)
	if err != nil || conn == nil {
		return VoteResponse{}, err
	}

	pbReq := &quorumv1.VoteRequest{
		Term:         uint64(req.Term),
		CandidateId:  req.CandidateID,
		LastLogIndex: int64(req.LastLogIndex),
		LastLogTerm:  int64(req.LastLogTerm),
		PreVote:      req.PreVote,
	}

	ctx, cancel := rpcContext()
	defer cancel()
	pbResp, err := quorumv1.NewRaftClient(conn).RequestVote(ctx, pbReq)
	if err != nil {
		return VoteResponse{}, err
	}

	return VoteResponse{
		Term:        int(pbResp.Term),
		VoteGranted: pbResp.VoteGranted,
		PreVote:     pbResp.PreVote,
	}, nil
}

func (t *GRPCTransport) AppendEntries(peerID string, req AppendRequest) (AppendResponse, error) {
	conn, err := t.getConn(peerID)
	if err != nil || conn == nil {
		return AppendResponse{}, err
	}

	pbReq := &quorumv1.AppendRequest{
		Term:         uint64(req.Term),
		LeaderId:     req.LeaderID,
		PrevLogIndex: int64(req.PrevLogIndex),
		PrevLogTerm:  int64(req.PrevLogTerm),
		LeaderCommit: int64(req.LeaderCommit),
		Entries:      logEntriesToProto(req.Entries),
	}

	ctx, cancel := rpcContext()
	defer cancel()
	pbResp, err := quorumv1.NewRaftClient(conn).AppendEntries(ctx, pbReq)
	if err != nil {
		return AppendResponse{}, err
	}

	return AppendResponse{
		Term:          int(pbResp.Term),
		Success:       pbResp.Success,
		ConflictTerm:  int(pbResp.ConflictTerm),
		ConflictIndex: int(pbResp.ConflictIndex),
	}, nil
}

func (t *GRPCTransport) InstallSnapshot(peerID string, req InstallSnapshotRequest) (InstallSnapshotResponse, error) {
	conn, err := t.getConn(peerID)
	if err != nil || conn == nil {
		return InstallSnapshotResponse{}, err
	}

	pbReq := &quorumv1.InstallSnapshotRequest{
		Term:              uint64(req.Term),
		LeaderId:          req.LeaderID,
		LastIncludedIndex: int64(req.LastIncludedIndex),
		LastIncludedTerm:  int64(req.LastIncludedTerm),
		SnapshotData:      req.SnapshotData,
	}

	ctx, cancel := rpcContext()
	defer cancel()
	pbResp, err := quorumv1.NewRaftClient(conn).InstallSnapshot(ctx, pbReq)
	if err != nil {
		return InstallSnapshotResponse{}, err
	}

	return InstallSnapshotResponse{
		Term:    int(pbResp.Term),
		Success: pbResp.Success,
	}, nil
}

func logEntriesToProto(entries []LogEntry) []*quorumv1.LogEntry {
	pb := make([]*quorumv1.LogEntry, len(entries))
	for i, e := range entries {
		pb[i] = &quorumv1.LogEntry{
			Index:   int64(e.Index),
			Term:    int64(e.Term),
			Command: e.Command,
		}
	}
	return pb
}
