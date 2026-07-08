package store

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	quorumv1 "quorum/gen/quorum/v1"
)

func putCmd(key, val string) []byte {
	req := &quorumv1.InternalRaftRequest{
		Cmd: &quorumv1.InternalRaftRequest_Put{
			Put: &quorumv1.PutRequest{Key: []byte(key), Value: []byte(val)},
		},
	}
	data, _ := proto.Marshal(req)
	return data
}

func delCmd(key string) []byte {
	req := &quorumv1.InternalRaftRequest{
		Cmd: &quorumv1.InternalRaftRequest_Delete{
			Delete: &quorumv1.DeleteRequest{Key: []byte(key)},
		},
	}
	data, _ := proto.Marshal(req)
	return data
}

func TestSetGetDelete(t *testing.T) {
	s := New()
	s.Apply(putCmd("foo", "bar"))

	val, rev, ok := s.Get("foo")
	require.True(t, ok)
	require.Equal(t, "bar", val)
	require.Equal(t, int64(1), rev)

	s.Apply(delCmd("foo"))
	_, _, ok = s.Get("foo")
	require.False(t, ok)

	s.Apply(delCmd("foo"))
}

func TestSetOverwrites(t *testing.T) {
	s := New()
	s.Apply(putCmd("foo", "bar"))
	val, _, ok := s.Get("foo")
	require.True(t, ok)
	require.Equal(t, "bar", val)

	s.Apply(putCmd("foo", "baz"))
	val, rev, ok := s.Get("foo")
	require.True(t, ok)
	require.Equal(t, "baz", val)
	require.Equal(t, int64(2), rev)
}

func TestConcurrentApply(t *testing.T) {
	s := New()

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.Apply(putCmd("foo", "val"))
		}(i)
	}

	wg.Wait()
	_, _, ok := s.Get("foo")
	require.True(t, ok)
}

func TestMalformedCommand(t *testing.T) {
	s := New()
	s.Apply([]byte("garbage"))
	_, _, ok := s.Get("foo")
	require.False(t, ok)
}

func TestDelNonExistent(t *testing.T) {
	s := New()
	s.Apply(delCmd("nonexistent"))
	_, _, ok := s.Get("nonexistent")
	require.False(t, ok)
}

func TestRevisionIncrements(t *testing.T) {
	s := New()
	require.Equal(t, int64(0), s.Revision())

	s.Apply(putCmd("a", "1"))
	require.Equal(t, int64(1), s.Revision())

	s.Apply(putCmd("b", "2"))
	require.Equal(t, int64(2), s.Revision())

	s.Apply(delCmd("a"))
	require.Equal(t, int64(3), s.Revision())
}

func TestTxnCompareEqual(t *testing.T) {
	s := New()
	s.Apply(putCmd("x", "hello"))

	txn := &quorumv1.InternalRaftRequest{
		Cmd: &quorumv1.InternalRaftRequest_Txn{
			Txn: &quorumv1.TxnRequest{
				Compare: []*quorumv1.Compare{
					{Key: []byte("x"), Type: quorumv1.Compare_EQUAL, Target: []byte("hello")},
				},
				Success: []*quorumv1.Operation{
					{Op: &quorumv1.Operation_Put{Put: &quorumv1.PutRequest{Key: []byte("x"), Value: []byte("world")}}},
				},
				Failure: []*quorumv1.Operation{
					{Op: &quorumv1.Operation_Put{Put: &quorumv1.PutRequest{Key: []byte("x"), Value: []byte("nope")}}},
				},
			},
		},
	}
	data, _ := proto.Marshal(txn)
	s.Apply(data)

	val, _, ok := s.Get("x")
	require.True(t, ok)
	require.Equal(t, "world", val)
}

func TestTxnCompareFails(t *testing.T) {
	s := New()
	s.Apply(putCmd("x", "hello"))

	txn := &quorumv1.InternalRaftRequest{
		Cmd: &quorumv1.InternalRaftRequest_Txn{
			Txn: &quorumv1.TxnRequest{
				Compare: []*quorumv1.Compare{
					{Key: []byte("x"), Type: quorumv1.Compare_EQUAL, Target: []byte("wrong")},
				},
				Success: []*quorumv1.Operation{
					{Op: &quorumv1.Operation_Put{Put: &quorumv1.PutRequest{Key: []byte("x"), Value: []byte("world")}}},
				},
				Failure: []*quorumv1.Operation{
					{Op: &quorumv1.Operation_Put{Put: &quorumv1.PutRequest{Key: []byte("y"), Value: []byte("fallback")}}},
				},
			},
		},
	}
	data, _ := proto.Marshal(txn)
	s.Apply(data)

	val, _, ok := s.Get("x")
	require.True(t, ok)
	require.Equal(t, "hello", val)

	val, _, ok = s.Get("y")
	require.True(t, ok)
	require.Equal(t, "fallback", val)

	succeeded, responses := s.LastTxnResult()
	require.False(t, succeeded)
	require.Len(t, responses, 1)
	require.NotNil(t, responses[0].GetPut())
	require.Equal(t, s.revision, responses[0].GetPut().Revision)
}

func TestTxnResultSucceeded(t *testing.T) {
	s := New()
	s.Apply(putCmd("x", "hello"))

	txn := &quorumv1.InternalRaftRequest{
		Cmd: &quorumv1.InternalRaftRequest_Txn{
			Txn: &quorumv1.TxnRequest{
				Compare: []*quorumv1.Compare{
					{Key: []byte("x"), Type: quorumv1.Compare_EQUAL, Target: []byte("hello")},
				},
				Success: []*quorumv1.Operation{
					{Op: &quorumv1.Operation_Put{Put: &quorumv1.PutRequest{Key: []byte("x"), Value: []byte("world")}}},
				},
				Failure: []*quorumv1.Operation{
					{Op: &quorumv1.Operation_Put{Put: &quorumv1.PutRequest{Key: []byte("y"), Value: []byte("fallback")}}},
				},
			},
		},
	}
	data, _ := proto.Marshal(txn)
	s.Apply(data)

	succeeded, responses := s.LastTxnResult()
	require.True(t, succeeded)
	require.Len(t, responses, 1)
	require.NotNil(t, responses[0].GetPut())
	require.Equal(t, s.revision, responses[0].GetPut().Revision)
}

func TestTxnResultDeleteOp(t *testing.T) {
	s := New()
	s.Apply(putCmd("x", "hello"))

	txn := &quorumv1.InternalRaftRequest{
		Cmd: &quorumv1.InternalRaftRequest_Txn{
			Txn: &quorumv1.TxnRequest{
				Success: []*quorumv1.Operation{
					{Op: &quorumv1.Operation_Delete{Delete: &quorumv1.DeleteRequest{Key: []byte("x")}}},
				},
			},
		},
	}
	data, _ := proto.Marshal(txn)
	s.Apply(data)

	succeeded, responses := s.LastTxnResult()
	require.True(t, succeeded)
	require.Len(t, responses, 1)
	require.NotNil(t, responses[0].GetDelete())
	require.Equal(t, s.revision, responses[0].GetDelete().Revision)
}

func TestTxnResultIsolated(t *testing.T) {
	s := New()
	s.Apply(putCmd("a", "1"))
	s.Apply(putCmd("b", "2"))

	succeeded, responses := s.LastTxnResult()
	require.False(t, succeeded)
	require.Nil(t, responses)
}

func TestRangeExact(t *testing.T) {
	s := New()
	s.Apply(putCmd("x", "hello"))
	s.Apply(putCmd("y", "world"))

	kvs, more := s.Range("x", "", 0)
	require.Len(t, kvs, 1)
	require.False(t, more)
	require.Equal(t, "x", string(kvs[0].Key))
	require.Equal(t, "hello", string(kvs[0].Value))

	kvs2, more2 := s.Range("z", "", 0)
	require.Nil(t, kvs2)
	require.False(t, more2)
}

func TestRangePrefix(t *testing.T) {
	s := New()
	s.Apply(putCmd("foo/bar", "1"))
	s.Apply(putCmd("foo/baz", "2"))
	s.Apply(putCmd("foo/qux", "3"))
	s.Apply(putCmd("bar", "4"))

	kvs, more := s.Range("foo/", "foo0", 0)
	require.Len(t, kvs, 3)
	require.False(t, more)
	require.Equal(t, "foo/bar", string(kvs[0].Key))
	require.Equal(t, "foo/baz", string(kvs[1].Key))
	require.Equal(t, "foo/qux", string(kvs[2].Key))
}

func TestRangePrefixLimit(t *testing.T) {
	s := New()
	s.Apply(putCmd("a/1", "x"))
	s.Apply(putCmd("a/2", "y"))
	s.Apply(putCmd("a/3", "z"))

	kvs, more := s.Range("a/", "a0", 2)
	require.Len(t, kvs, 2)
	require.True(t, more)
}

func TestRangeNoMatch(t *testing.T) {
	s := New()
	s.Apply(putCmd("foo/bar", "1"))

	kvs, more := s.Range("baz/", "baz0", 0)
	require.Nil(t, kvs)
	require.False(t, more)
}

func TestEventsSince(t *testing.T) {
	s := New()
	s.Apply(putCmd("a", "1"))
	s.Apply(putCmd("b", "2"))

	events := s.EventsSince(0, "")
	require.Len(t, events, 2)
	require.Equal(t, quorumv1.Event_PUT, events[0].Type)
	require.Equal(t, "a", string(events[0].Key))
	require.Equal(t, int64(1), events[0].Revision)
	require.Equal(t, "b", string(events[1].Key))
	require.Equal(t, int64(2), events[1].Revision)

	events = s.EventsSince(1, "")
	require.Len(t, events, 2)
	require.Equal(t, "a", string(events[0].Key))
	require.Equal(t, "b", string(events[1].Key))
}

func TestEventsSinceDelete(t *testing.T) {
	s := New()
	s.Apply(putCmd("x", "hello"))
	s.Apply(delCmd("x"))

	events := s.EventsSince(0, "")
	require.Len(t, events, 2)
	require.Equal(t, quorumv1.Event_PUT, events[0].Type)
	require.Equal(t, quorumv1.Event_DELETE, events[1].Type)
	require.Nil(t, events[1].Value)

	events = s.EventsSince(2, "")
	require.Len(t, events, 1)
	require.Equal(t, quorumv1.Event_DELETE, events[0].Type)
}

func TestEventsSinceRingBuffer(t *testing.T) {
	s := New()
	for i := 0; i < MaxEvents+10; i++ {
		s.Apply(putCmd(fmt.Sprintf("k%d", i), "v"))
	}

	events := s.EventsSince(0, "")
	require.Len(t, events, MaxEvents)
	require.Equal(t, "k10", string(events[0].Key))
}

func TestEventsSinceTxn(t *testing.T) {
	s := New()
	s.Apply(putCmd("x", "hello"))

	txn := &quorumv1.InternalRaftRequest{
		Cmd: &quorumv1.InternalRaftRequest_Txn{
			Txn: &quorumv1.TxnRequest{
				Compare: []*quorumv1.Compare{
					{Key: []byte("x"), Type: quorumv1.Compare_EQUAL, Target: []byte("hello")},
				},
				Success: []*quorumv1.Operation{
					{Op: &quorumv1.Operation_Put{Put: &quorumv1.PutRequest{Key: []byte("x"), Value: []byte("world")}}},
					{Op: &quorumv1.Operation_Delete{Delete: &quorumv1.DeleteRequest{Key: []byte("y")}}},
				},
			},
		},
	}
	data, _ := proto.Marshal(txn)
	s.Apply(data)

	events := s.EventsSince(1, "")
	require.Len(t, events, 3)
	require.Equal(t, quorumv1.Event_PUT, events[0].Type)
	require.Equal(t, "x", string(events[0].Key))
	require.Equal(t, "hello", string(events[0].Value))
	require.Equal(t, int64(1), events[0].Revision)
	require.Equal(t, quorumv1.Event_PUT, events[1].Type)
	require.Equal(t, "x", string(events[1].Key))
	require.Equal(t, "world", string(events[1].Value))
	require.Equal(t, quorumv1.Event_DELETE, events[2].Type)
	require.Equal(t, "y", string(events[2].Key))
}

func TestSnapshotRestore(t *testing.T) {
	s := New()
	s.Apply(putCmd("a", "1"))
	s.Apply(putCmd("b", "2"))
	s.Apply(putCmd("c", "3"))

	data, err := s.Snapshot()
	require.NoError(t, err)

	s2 := New()
	err = s2.Restore(data)
	require.NoError(t, err)
	require.Equal(t, int64(3), s2.Revision())

	val, _, ok := s2.Get("a")
	require.True(t, ok)
	require.Equal(t, "1", val)
	val, _, ok = s2.Get("b")
	require.True(t, ok)
	require.Equal(t, "2", val)
	val, _, ok = s2.Get("c")
	require.True(t, ok)
	require.Equal(t, "3", val)

}

func TestSnapshotRestoreEmpty(t *testing.T) {
	s := New()
	data, err := s.Snapshot()
	require.NoError(t, err)

	s2 := New()
	err = s2.Restore(data)
	require.NoError(t, err)
	require.Equal(t, int64(0), s2.Revision())
}

func TestRestoreOverwrites(t *testing.T) {
	s := New()
	s.Apply(putCmd("a", "1"))
	s.Apply(putCmd("b", "2"))
	data, err := s.Snapshot()
	require.NoError(t, err)

	s2 := New()
	s2.Apply(putCmd("x", "99"))
	err = s2.Restore(data)
	require.NoError(t, err)

	_, _, ok := s2.Get("x")
	require.False(t, ok, "restore should overwrite existing data")

	val, _, ok := s2.Get("a")
	require.True(t, ok)
	require.Equal(t, "1", val)
	require.Equal(t, int64(2), s2.Revision())
}

func TestEventsSincePrefixMatch(t *testing.T) {
	s := New()
	s.Apply(putCmd("/containers/nginx/spec", "nginx"))
	s.Apply(putCmd("/containers/redis/spec", "redis"))
	s.Apply(putCmd("/nodes/worker1/heartbeat", "ping"))

	events := s.EventsSince(0, "/containers/")
	require.Len(t, events, 2)
	require.Equal(t, "/containers/nginx/spec", string(events[0].Key))
	require.Equal(t, "/containers/redis/spec", string(events[1].Key))

	events = s.EventsSince(0, "/nodes/worker1/assignments/")
	require.Len(t, events, 0)
}
