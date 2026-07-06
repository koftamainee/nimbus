package wal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenNotExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.wal")
	w, err := Open(path)
	require.NoError(t, err)

	_, err = os.Stat(path)
	require.NoError(t, err, "file should exist")

	err = w.Close()
	require.NoError(t, err)
}

func TestOpenExistingFile(t *testing.T) {
	const testData = "testdata"
	path := filepath.Join(t.TempDir(), "new.wal")
	err := os.MkdirAll(filepath.Dir(path), 0777)
	require.NoError(t, err)

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)

	_, err = file.WriteString(testData)
	require.NoError(t, err)
	err = file.Close()
	require.NoError(t, err)

	w, err := Open(path)
	require.NoError(t, err)
	err = w.Append(Entry{Operation: OpSet, Key: "x", Value: "1"})
	require.NoError(t, err)

	_, err = os.Stat(path)
	require.NoError(t, err, "file should exist")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), testData)
	assert.Contains(t, string(data), `"SET"`)

	err = w.Close()
	require.NoError(t, err)
}

func TestAppendAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.wal")
	w, err := Open(path)
	require.NoError(t, err)

	operations := make([]Entry, 3)
	operations[0] = Entry{Operation: OpSet, Key: "foo", Value: "bar"}
	operations[1] = Entry{Operation: OpSet, Key: "bar", Value: "foo"}
	operations[2] = Entry{Operation: OpDel, Key: "foo"}

	var b strings.Builder
	for _, e := range operations {
		err = json.NewEncoder(&b).Encode(e)
		require.NoError(t, err)
	}
	expectedData := b.String()

	for _, operation := range operations {
		err := w.Append(operation)
		require.NoError(t, err)
	}

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, expectedData, string(data))

	err = w.Close()
	require.NoError(t, err)

	w, err = Open(path)
	require.NoError(t, err)

	i := 0
	apply := func(e Entry) error {
		require.Equal(t, operations[i], e)
		i++
		return nil
	}

	err = w.Replay(apply)
	require.NoError(t, err)
	err = w.Close()
	require.NoError(t, err)
}

func TestConcurrentAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.wal")
	w, err := Open(path)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			e := Entry{Operation: OpSet, Key: "k", Value: strconv.Itoa(n)}
			w.Append(e)
		}(i)
	}
	wg.Wait()
	err = w.Close()
	require.NoError(t, err)

	count := 0
	w, err = Open(path)
	require.NoError(t, err)
	apply := func(e Entry) error {
		count++
		return nil
	}
	err = w.Replay(apply)
	require.NoError(t, err)

	err = w.Close()
	require.NoError(t, err)
	require.Equal(t, 100, count)
}

func TestReplayCorruptedEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.wal")
	err := os.WriteFile(path, []byte("garbagevalue\n"+`{"op": "SET", "key": "foo", "value": "bar"}`), 0644)
	require.NoError(t, err)
	w, err := Open(path)
	require.NoError(t, err)

	count := 0
	apply := func(e Entry) error {
		count++
		require.Equal(t, Entry{Operation: OpSet, Key: "foo", Value: "bar"}, e)
		return nil
	}

	err = w.Replay(apply)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	err = w.Close()
	require.NoError(t, err)
}
