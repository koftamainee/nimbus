package store

import (
	"sync"

	"google.golang.org/protobuf/proto"

	quorumv1 "quorum/gen/quorum/v1"
)

type Value struct {
	Value    string
	Revision int64
}

type Store struct {
	mu       sync.RWMutex
	data     map[string]*Value
	revision int64
}

func New() *Store {
	return &Store{data: make(map[string]*Value)}
}

func (s *Store) Apply(cmd []byte) {
	var req quorumv1.InternalRaftRequest
	if err := proto.Unmarshal(cmd, &req); err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.revision++

	switch r := req.Cmd.(type) {
	case *quorumv1.InternalRaftRequest_Put:
		s.data[string(r.Put.Key)] = &Value{
			Value:    string(r.Put.Value),
			Revision: s.revision,
		}

	case *quorumv1.InternalRaftRequest_Delete:
		delete(s.data, string(r.Delete.Key))

	case *quorumv1.InternalRaftRequest_Txn:
		s.applyTxn(r.Txn)
	}
}

func (s *Store) applyTxn(txn *quorumv1.TxnRequest) {
	ok := true
	for _, cmp := range txn.Compare {
		if !s.evalCompare(cmp) {
			ok = false
			break
		}
	}

	ops := txn.Failure
	if ok {
		ops = txn.Success
	}

	for _, op := range ops {
		switch o := op.Op.(type) {
		case *quorumv1.Operation_Put:
			s.data[string(o.Put.Key)] = &Value{
				Value:    string(o.Put.Value),
				Revision: s.revision,
			}
		case *quorumv1.Operation_Delete:
			delete(s.data, string(o.Delete.Key))
		}
	}
}

func (s *Store) evalCompare(cmp *quorumv1.Compare) bool {
	v, ok := s.data[string(cmp.Key)]
	if !ok {
		return cmp.Type == quorumv1.Compare_NOT_EQUAL && len(cmp.Target) == 0
	}

	switch cmp.Type {
	case quorumv1.Compare_EQUAL:
		return v.Value == string(cmp.Target)
	case quorumv1.Compare_NOT_EQUAL:
		return v.Value != string(cmp.Target)
	case quorumv1.Compare_GREATER:
		return v.Value > string(cmp.Target)
	case quorumv1.Compare_LESS:
		return v.Value < string(cmp.Target)
	default:
		return false
	}
}

func (s *Store) Get(key string) (string, int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	if !ok {
		return "", 0, false
	}
	return v.Value, v.Revision, true
}

func (s *Store) Revision() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revision
}
