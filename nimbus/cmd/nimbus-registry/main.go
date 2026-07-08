package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
)

type ImageInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func main() {
	addr := flag.String("addr", ":9091", "listen address")
	imageDir := flag.String("image-dir", "./images", "image storage directory")
	flag.Parse()

	logger := slog.With("component", "nimbus-registry")

	os.MkdirAll(*imageDir, 0755)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /images/{name}", imageGetHandler(*imageDir))
	mux.HandleFunc("PUT /images/{name}", imagePutHandler(*imageDir))

	logger.Info("starting image registry", "addr", *addr, "image-dir", *imageDir)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

func imageGetHandler(imageDir string) http.HandlerFunc {
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

func imagePutHandler(imageDir string) http.HandlerFunc {
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
