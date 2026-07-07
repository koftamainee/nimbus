package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

type ImageInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func ImageGetHandler(imageDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			http.Error(w, "missing image name", http.StatusBadRequest)
			return
		}
		path := filepath.Join(imageDir, name+".tar")
		http.ServeFile(w, r, path)
	}
}

func ImagePutHandler(imageDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			http.Error(w, "missing image name", http.StatusBadRequest)
			return
		}

		os.MkdirAll(imageDir, 0755)
		path := filepath.Join(imageDir, name+".tar")

		f, err := os.Create(path)
		if err != nil {
			http.Error(w, fmt.Sprintf("cannot create file: %s", err), http.StatusInternalServerError)
			return
		}
		defer f.Close()

		if _, err := io.Copy(f, r.Body); err != nil {
			http.Error(w, fmt.Sprintf("cannot write file: %s", err), http.StatusInternalServerError)
			return
		}

		info := ImageInfo{
			Name: name,
			Path: path,
			Size: 0,
		}
		if fi, err := f.Stat(); err == nil {
			info.Size = fi.Size()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	}
}
