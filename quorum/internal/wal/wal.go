package wal

import (
	"bufio"
	"log"
	"os"
	"sync"
)

type WAL struct {
	mu   sync.Mutex
	file *os.File
}

func Open(path string) (*WAL, error) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	return &WAL{file: file}, nil
}

func (w *WAL) Close() error {
	return w.file.Close()
}

func (w *WAL) Append(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.file.Write(data); err != nil {
		return err
	}
	if _, err := w.file.Write([]byte("\n")); err != nil {
		return err
	}
	return w.file.Sync()
}

func (w *WAL) Replay(apply func([]byte) error) error {
	file, err := os.Open(w.file.Name())
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		data := make([]byte, len(scanner.Bytes()))
		copy(data, scanner.Bytes())
		if err := apply(data); err != nil {
			log.Printf("Error applying entry from %s: %s\n", w.file.Name(), err)
		}
	}
	return scanner.Err()
}
