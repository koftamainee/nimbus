package wal

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"sync"
)

type WAL struct {
	mu       sync.Mutex
	file     *os.File
	filePath string
}

func Open(path string) (*WAL, error) {
	os.MkdirAll(filepath.Dir(path), 0755)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	return &WAL{file: file, filePath: path}, nil
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
	file, err := os.Open(w.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		data := make([]byte, len(scanner.Bytes()))
		copy(data, scanner.Bytes())
		if err := apply(data); err != nil {
			log.Printf("Error applying entry from %s: %s\n", w.filePath, err)
		}
	}
	return scanner.Err()
}

func (w *WAL) Truncate(keep func([]byte) bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.file.Close(); err != nil {
		return err
	}

	raw, err := w.readAll()
	if err != nil {
		return err
	}

	tmpPath := w.filePath + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	for _, data := range raw {
		if keep(data) {
			if _, err := file.Write(data); err != nil {
				file.Close()
				return err
			}
			if _, err := file.Write([]byte("\n")); err != nil {
				file.Close()
				return err
			}
		}
	}

	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, w.filePath); err != nil {
		return err
	}

	file, err = os.OpenFile(w.filePath, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	w.file = file
	return nil
}

func (w *WAL) readAll() ([][]byte, error) {
	file, err := os.Open(w.filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var raw [][]byte
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		data := make([]byte, len(scanner.Bytes()))
		copy(data, scanner.Bytes())
		raw = append(raw, data)
	}
	return raw, scanner.Err()
}

func (w *WAL) Count() (int, error) {
	raw, err := w.readAll()
	if err != nil {
		return 0, err
	}
	return len(raw), nil
}
