package store

import (
	"quorum/internal/wal"
	"sync"
)

type Store struct {
	mu   sync.RWMutex
	data map[string]string
	wal  *wal.WAL
}

func New(w *wal.WAL) (*Store, error) {
	store := &Store{data: make(map[string]string), wal: w}

	apply := func(entry wal.Entry) error {
		switch entry.Operation {
		case wal.OpSet:
			store.data[entry.Key] = entry.Value

		case wal.OpDel:
			delete(store.data, entry.Key)
		}
		return nil
	}

	err := w.Replay(apply)
	if err != nil {
		return nil, err
	}

	return store, nil
}

func (store *Store) Set(key string, value string) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	err := store.wal.Append(wal.Entry{Operation: wal.OpSet, Key: key, Value: value})
	if err != nil {
		return err
	}

	store.data[key] = value
	return nil
}

func (store *Store) Get(key string) (string, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	val, ok := store.data[key]
	return val, ok
}

func (store *Store) Delete(key string) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	err := store.wal.Append(wal.Entry{Operation: wal.OpDel, Key: key})
	if err != nil {
		return err
	}

	delete(store.data, key)
	return nil
}
