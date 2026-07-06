package store

import (
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
}
