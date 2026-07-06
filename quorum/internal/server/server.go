package server

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"quorum/gen/quorum/v1"
	"quorum/internal/raft"
	"quorum/internal/store"
)

type RaftServer struct {
	quorumv1.UnimplementedRaftServer
	r *raft.Raft
}

func NewRaftServer(r *raft.Raft) *RaftServer {
	return &RaftServer{r: r}
}

func (s *RaftServer) RequestVote(ctx context.Context, req *quorumv1.VoteRequest) (*quorumv1.VoteResponse, error) {
	resp := s.r.HandleRequestVote(raft.VoteRequest{
		Term:         int(req.Term),
		CandidateID:  req.CandidateId,
		LastLogIndex: int(req.LastLogIndex),
		LastLogTerm:  int(req.LastLogTerm),
	})
	return &quorumv1.VoteResponse{
		Term:        uint64(resp.Term),
		VoteGranted: resp.VoteGranted,
	}, nil
}

func (s *RaftServer) AppendEntries(ctx context.Context, req *quorumv1.AppendRequest) (*quorumv1.AppendResponse, error) {
	entries := make([]raft.LogEntry, len(req.Entries))
	for i, e := range req.Entries {
		entries[i] = raft.LogEntry{
			Index:   int(e.Index),
			Term:    int(e.Term),
			Command: e.Command,
		}
	}

	resp := s.r.HandleAppendEntries(raft.AppendRequest{
		Term:         int(req.Term),
		LeaderID:     req.LeaderId,
		PrevLogIndex: int(req.PrevLogIndex),
		PrevLogTerm:  int(req.PrevLogTerm),
		Entries:      entries,
		LeaderCommit: int(req.LeaderCommit),
	})
	return &quorumv1.AppendResponse{
		Term:          uint64(resp.Term),
		Success:       resp.Success,
		ConflictTerm:  int64(resp.ConflictTerm),
		ConflictIndex: int64(resp.ConflictIndex),
	}, nil
}

type KVServer struct {
	quorumv1.UnimplementedKVServer
	r     *raft.Raft
	store *store.Store
}

func NewKVServer(r *raft.Raft, st *store.Store) *KVServer {
	return &KVServer{r: r, store: st}
}

func (s *KVServer) Put(ctx context.Context, req *quorumv1.PutRequest) (*quorumv1.PutResponse, error) {
	cmd := &quorumv1.InternalRaftRequest{Cmd: &quorumv1.InternalRaftRequest_Put{Put: req}}
	data, _ := proto.Marshal(cmd)

	if err := s.r.Propose(ctx, data); err != nil {
		if notLeader, ok := errors.AsType[raft.ErrNotLeader](err); ok {
			return nil, status.Errorf(codes.Unavailable, "leader: %s", notLeader.LeaderID)
		}
		return nil, err
	}

	return &quorumv1.PutResponse{Revision: s.store.Revision()}, nil
}

func (s *KVServer) Get(ctx context.Context, req *quorumv1.GetRequest) (*quorumv1.GetResponse, error) {
	if err := s.r.ReadIndex(ctx); err != nil {
		if notLeader, ok := errors.AsType[raft.ErrNotLeader](err); ok {
			return nil, status.Errorf(codes.Unavailable, "leader: %s", notLeader.LeaderID)
		}
		return nil, err
	}

	val, rev, ok := s.store.Get(string(req.Key))
	if !ok {
		return &quorumv1.GetResponse{Found: false}, nil
	}
	return &quorumv1.GetResponse{Value: []byte(val), Revision: rev, Found: true}, nil
}

func (s *KVServer) Delete(ctx context.Context, req *quorumv1.DeleteRequest) (*quorumv1.DeleteResponse, error) {
	cmd := &quorumv1.InternalRaftRequest{Cmd: &quorumv1.InternalRaftRequest_Delete{Delete: req}}
	data, _ := proto.Marshal(cmd)

	if err := s.r.Propose(ctx, data); err != nil {
		if notLeader, ok := errors.AsType[raft.ErrNotLeader](err); ok {
			return nil, status.Errorf(codes.Unavailable, "leader: %s", notLeader.LeaderID)
		}
		return nil, err
	}

	return &quorumv1.DeleteResponse{Revision: s.store.Revision()}, nil
}

func (s *KVServer) Txn(ctx context.Context, req *quorumv1.TxnRequest) (*quorumv1.TxnResponse, error) {
	cmd := &quorumv1.InternalRaftRequest{Cmd: &quorumv1.InternalRaftRequest_Txn{Txn: req}}
	data, _ := proto.Marshal(cmd)

	if err := s.r.Propose(ctx, data); err != nil {
		if notLeader, ok := errors.AsType[raft.ErrNotLeader](err); ok {
			return nil, status.Errorf(codes.Unavailable, "leader: %s", notLeader.LeaderID)
		}
		return nil, err
	}

	succeeded, responses := s.store.LastTxnResult()
	return &quorumv1.TxnResponse{
		Succeeded: succeeded,
		Responses: responses,
		Revision:  s.store.Revision(),
	}, nil
}

func (s *KVServer) Range(ctx context.Context, req *quorumv1.RangeRequest) (*quorumv1.RangeResponse, error) {
	if err := s.r.ReadIndex(ctx); err != nil {
		if notLeader, ok := errors.AsType[raft.ErrNotLeader](err); ok {
			return nil, status.Errorf(codes.Unavailable, "leader: %s", notLeader.LeaderID)
		}
		return nil, err
	}

	kvs, more := s.store.Range(string(req.Key), string(req.RangeEnd), req.Limit)
	return &quorumv1.RangeResponse{Kvs: kvs, More: more}, nil
}

type WatchServer struct {
	quorumv1.UnimplementedWatchServer
	r     *raft.Raft
	store *store.Store
}

func NewWatchServer(r *raft.Raft, st *store.Store) *WatchServer {
	return &WatchServer{r: r, store: st}
}

func (s *WatchServer) Watch(req *quorumv1.WatchRequest, stream quorumv1.Watch_WatchServer) error {
	rev := req.StartRevision
	if rev == 0 {
		rev = s.store.Revision() + 1
	}
	key := string(req.Key)

	events := s.store.EventsSince(rev, key)
	if len(events) > 0 {
		if err := stream.Send(&quorumv1.WatchResponse{Events: events}); err != nil {
			return err
		}
		rev = events[len(events)-1].Revision + 1
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-ticker.C:
			events := s.store.EventsSince(rev, key)
			if len(events) > 0 {
				if err := stream.Send(&quorumv1.WatchResponse{Events: events}); err != nil {
					return err
				}
				rev = events[len(events)-1].Revision + 1
			}
		}
	}
}
