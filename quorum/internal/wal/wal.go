package wal

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"sync"
)

type Operation string

const OpSet Operation = "SET"
const OpDel Operation = "DEL"

type Entry struct {
	Operation Operation `json:"op"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
}

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

func (wal *WAL) Close() error {
	return wal.file.Close()
}

func (wal *WAL) Append(entry Entry) error {
	wal.mu.Lock()
	defer wal.mu.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := wal.file.Write(data); err != nil {
		return err
	}
	if _, err := wal.file.Write([]byte("\n")); err != nil {
		return err
	}
	return wal.file.Sync()
}

func (wal *WAL) Replay(apply func(entry Entry) error) error {
	file, err := os.Open(wal.file.Name())
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			log.Printf("Error parsing entry from %s: %s\n", wal.file.Name(), err)
			continue
		}
		err = apply(entry)
		if err != nil {
			log.Printf("Error applying entry from %s: %s\n", wal.file.Name(), err)
		}
	}
	return scanner.Err()
}
