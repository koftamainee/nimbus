package raft

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Snapshotter interface {
	Save(index, term int, data []byte) error
	Load() (index, term int, data []byte, err error)
}

type FileSnapshotter struct {
	dir string
}

func NewFileSnapshotter(dir string) *FileSnapshotter {
	os.MkdirAll(dir, 0755)
	return &FileSnapshotter{dir: dir}
}

func (fs *FileSnapshotter) Save(index, term int, data []byte) error {
	name := fmt.Sprintf("snapshot-%d-%d.dat", index, term)
	path := filepath.Join(fs.dir, name)
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}

	if err := os.Rename(tmp, path); err != nil {
		return err
	}

	old, err := fs.list()
	if err != nil {
		return err
	}

	for _, f := range old {
		if f != name {
			os.Remove(filepath.Join(fs.dir, f))
		}
	}

	return nil
}

func (fs *FileSnapshotter) Load() (int, int, []byte, error) {
	files, err := fs.list()
	if err != nil {
		return 0, 0, nil, err
	}

	if len(files) == 0 {
		return 0, 0, nil, nil
	}

	latest := files[len(files)-1]
	index, term, err := parseSnapshotName(latest)
	if err != nil {
		return 0, 0, nil, err
	}

	data, err := os.ReadFile(filepath.Join(fs.dir, latest))
	if err != nil {
		return 0, 0, nil, err
	}

	return index, term, data, nil
}

func (fs *FileSnapshotter) list() ([]string, error) {
	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "snapshot-") {
			files = append(files, e.Name())
		}
	}

	sort.Slice(files, func(i, j int) bool {
		ii, _, _ := parseSnapshotName(files[i])
		ji, _, _ := parseSnapshotName(files[j])
		return ii < ji
	})

	return files, nil
}

func parseSnapshotName(name string) (int, int, error) {
	name = strings.TrimPrefix(name, "snapshot-")
	name = strings.TrimSuffix(name, ".dat")
	parts := strings.SplitN(name, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid snapshot name: %s", name)
	}
	index, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	term, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return index, term, nil
}
