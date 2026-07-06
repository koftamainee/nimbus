package main

import (
	"io"
	"log"
	"net/http"

	"quorum/internal/store"
	"quorum/internal/wal"
)

func main() {
	w, err := wal.Open("quorum.wal")
	if err != nil {
		log.Fatal(err)
	}

	defer w.Close()

	s, err := store.New(w)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/kv/", func(rw http.ResponseWriter, r *http.Request) {
		key := r.URL.Path[len("/kv/"):]

		switch r.Method {
		case http.MethodGet:
			v, ok := s.Get(key)
			if !ok {
				http.NotFound(rw, r)
				return
			}
			rw.Write([]byte(v))

		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			if err := s.Set(key, string(body)); err != nil {
				http.Error(rw, err.Error(), 500)
				return
			}
			rw.WriteHeader(http.StatusOK)

		case http.MethodDelete:
			if err := s.Delete(key); err != nil {
				http.Error(rw, err.Error(), 500)
				return
			}
			rw.WriteHeader(http.StatusOK)
		}
	})

	log.Println("quorum listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
