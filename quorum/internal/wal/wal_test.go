package wal

import (
	"os"
	"path/filepath"
	"strconv"
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
	err = w.Append([]byte(`{"op":"SET","key":"x","value":"1"}`))
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

	operations := [][]byte{
		[]byte(`{"op":"SET","key":"foo","value":"bar"}`),
		[]byte(`{"op":"SET","key":"bar","value":"foo"}`),
		[]byte(`{"op":"DEL","key":"foo"}`),
	}

	var expected []byte
	for _, op := range operations {
		expected = append(expected, op...)
		expected = append(expected, '\n')
	}

	for _, op := range operations {
		err := w.Append(op)
		require.NoError(t, err)
	}

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(expected), string(data))

	err = w.Close()
	require.NoError(t, err)

	w, err = Open(path)
	require.NoError(t, err)

	i := 0
	apply := func(data []byte) error {
		require.Equal(t, operations[i], data)
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
			w.Append([]byte(`{"op":"SET","key":"k","value":"` + strconv.Itoa(n) + `"}`))
		}(i)
	}
	wg.Wait()
	err = w.Close()
	require.NoError(t, err)

	count := 0
	w, err = Open(path)
	require.NoError(t, err)
	apply := func([]byte) error {
		count++
		return nil
	}
	err = w.Replay(apply)
	require.NoError(t, err)

	err = w.Close()
	require.NoError(t, err)
	require.Equal(t, 100, count)
}

func TestReplayAllLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.wal")
	err := os.WriteFile(path, []byte("line1\nline2\n"), 0644)
	require.NoError(t, err)
	w, err := Open(path)
	require.NoError(t, err)

	var lines [][]byte
	apply := func(data []byte) error {
		lines = append(lines, data)
		return nil
	}

	err = w.Replay(apply)
	require.NoError(t, err)
	require.Equal(t, 2, len(lines))
	require.Equal(t, "line1", string(lines[0]))
	require.Equal(t, "line2", string(lines[1]))

	err = w.Close()
	require.NoError(t, err)
}
