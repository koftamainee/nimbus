package store

import (
	"sort"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"

	quorumv1 "quorum/gen/quorum/v1"
)

const MaxEvents = 1000

type Value struct {
	Value    string
	Revision int64
}

type Store struct {
	mu       sync.RWMutex
	data     map[string]*Value
	revision int64

	lastTxnSucceeded bool
	lastTxnResponses []*quorumv1.OperationResponse

	events []*quorumv1.Event
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
		s.appendEvent(quorumv1.Event_PUT, r.Put.Key, r.Put.Value)

	case *quorumv1.InternalRaftRequest_Delete:
		delete(s.data, string(r.Delete.Key))
		s.appendEvent(quorumv1.Event_DELETE, r.Delete.Key, nil)

	case *quorumv1.InternalRaftRequest_Txn:
		s.applyTxn(r.Txn)
	}
}

func (s *Store) appendEvent(typ quorumv1.Event_EventType, key, value []byte) {
	s.events = append(s.events, &quorumv1.Event{
		Type:     typ,
		Key:      key,
		Value:    value,
		Revision: s.revision,
	})
	if len(s.events) > MaxEvents {
		s.events = s.events[len(s.events)-MaxEvents:]
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

	resps := make([]*quorumv1.OperationResponse, 0, len(ops))
	for _, op := range ops {
		switch o := op.Op.(type) {
		case *quorumv1.Operation_Put:
			s.data[string(o.Put.Key)] = &Value{
				Value:    string(o.Put.Value),
				Revision: s.revision,
			}
			resps = append(resps, &quorumv1.OperationResponse{
				Resp: &quorumv1.OperationResponse_Put{
					Put: &quorumv1.PutResponse{Revision: s.revision},
				},
			})
			s.appendEvent(quorumv1.Event_PUT, o.Put.Key, o.Put.Value)
		case *quorumv1.Operation_Delete:
			delete(s.data, string(o.Delete.Key))
			resps = append(resps, &quorumv1.OperationResponse{
				Resp: &quorumv1.OperationResponse_Delete{
					Delete: &quorumv1.DeleteResponse{Revision: s.revision},
				},
			})
			s.appendEvent(quorumv1.Event_DELETE, o.Delete.Key, nil)
		}
	}

	s.lastTxnSucceeded = ok
	s.lastTxnResponses = resps
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

func (s *Store) Range(key, rangeEnd string, limit int64) ([]*quorumv1.KeyValue, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if rangeEnd == "" {
		v, ok := s.data[key]
		if !ok {
			return nil, false
		}
		return []*quorumv1.KeyValue{
			{Key: []byte(key), Value: []byte(v.Value), Revision: v.Revision},
		}, false
	}

	var kvs []*quorumv1.KeyValue
	for k, v := range s.data {
		if k >= key && k < rangeEnd {
			kvs = append(kvs, &quorumv1.KeyValue{
				Key:      []byte(k),
				Value:    []byte(v.Value),
				Revision: v.Revision,
			})
		}
	}

	sort.Slice(kvs, func(i, j int) bool {
		return string(kvs[i].Key) < string(kvs[j].Key)
	})

	more := false
	if limit > 0 && int64(len(kvs)) > limit {
		kvs = kvs[:limit]
		more = true
	}

	return kvs, more
}

func (s *Store) EventsSince(rev int64, key string) []*quorumv1.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	start := -1
	for i, e := range s.events {
		if e.Revision >= rev {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}

	if key == "" {
		out := make([]*quorumv1.Event, len(s.events)-start)
		copy(out, s.events[start:])
		return out
	}

	out := make([]*quorumv1.Event, 0, len(s.events)-start)
	for _, e := range s.events[start:] {
		if strings.HasPrefix(string(e.Key), key) {
			out = append(out, e)
		}
	}
	return out
}

func (s *Store) LastTxnResult() (bool, []*quorumv1.OperationResponse) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastTxnSucceeded, s.lastTxnResponses
}

func (s *Store) Revision() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revision
}
