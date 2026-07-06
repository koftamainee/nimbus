package store

import (
	"path/filepath"
	"quorum/internal/wal"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func createTempStore(t *testing.T) *Store {
	t.Helper()
	walPath := filepath.Join(t.TempDir(), "new.wal")
	w, err := wal.Open(walPath)
	require.NoError(t, err)
	return &Store{
		mu:   sync.RWMutex{},
		data: make(map[string]string),
		wal:  w,
	}
}

func TestSetGetDelete(t *testing.T) {
	s := createTempStore(t)
	err := s.Set("foo", "bar")
	require.NoError(t, err)

	data, ok := s.Get("foo")
	require.True(t, ok)
	require.Equal(t, "bar", data)

	err = s.Delete("foo")
	require.NoError(t, err)
	data, ok = s.Get("foo")
	require.False(t, ok)
	require.Equal(t, "", data)

	err = s.Delete("foo")
	require.NoError(t, err)

	err = s.wal.Close()
	require.NoError(t, err)
}

func TestSetOverwrites(t *testing.T) {
	s := createTempStore(t)
	err := s.Set("foo", "bar")
	require.NoError(t, err)
	data, ok := s.Get("foo")
	require.True(t, ok)
	require.Equal(t, "bar", data)

	err = s.Set("foo", "baz")
	require.NoError(t, err)
	data, ok = s.Get("foo")
	require.True(t, ok)
	require.Equal(t, "baz", data)

	err = s.wal.Close()
	require.NoError(t, err)
}

func TestReplayProducesSameState(t *testing.T) {
	s := createTempStore(t)
	err := s.Set("a", "1")
	require.NoError(t, err)
	err = s.Set("b", "2")
	require.NoError(t, err)
	err = s.Delete("a")
	require.NoError(t, err)
	err = s.Set("b", "3")

	s2, err := New(s.wal)
	require.NoError(t, err)

	require.Equal(t, s2.data, s.data)

	err = s.wal.Close()
	require.NoError(t, err)
}

func TestConcurrentSetReplayMatchesMemory(t *testing.T) {
	s := createTempStore(t)

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.Set("foo", strconv.Itoa(n))
		}(i)
	}

	wg.Wait()
	_, ok := s.Get("foo")
	require.True(t, ok)

	s2, err := New(s.wal)
	require.NoError(t, err)
	require.Equal(t, s2.data, s.data)
}
