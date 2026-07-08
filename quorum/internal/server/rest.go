package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	quorumv1 "quorum/gen/quorum/v1"
)

type ContainerSpec struct {
	Name     string   `json:"name"`
	Image    string   `json:"image"`
	Memory   string   `json:"memory,omitempty"`
	Cpus     float64  `json:"cpus,omitempty"`
	Env      []string `json:"env,omitempty"`
	Cmd      []string `json:"cmd,omitempty"`
	Replicas int      `json:"replicas"`
}

type ContainerInfo struct {
	Name   string `json:"name"`
	NodeID string `json:"node_id,omitempty"`
	Status string `json:"status,omitempty"`
}

type NodeInfo struct {
	ID       string   `json:"id"`
	Hostname string   `json:"hostname"`
	Addr     string   `json:"addr"`
	Labels   []string `json:"labels,omitempty"`
}

type LogEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ScaleRequest struct {
	Replicas int `json:"replicas"`
}

type apiError struct {
	Error  string `json:"error"`
	Leader string `json:"leader,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

func writeLeaderRedirect(w http.ResponseWriter, leaderID string) {
	writeJSON(w, http.StatusServiceUnavailable, apiError{
		Error:  "not leader",
		Leader: leaderID,
	})
}

func isLeaderUnavailable(err error) (string, bool) {
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unavailable {
		return "", false
	}
	msg := st.Message()
	if strings.HasPrefix(msg, "leader: ") {
		return strings.TrimPrefix(msg, "leader: "), true
	}
	return "", false
}

func prefixEnd(key []byte) []byte {
	end := make([]byte, len(key))
	copy(end, key)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] < 0xFF {
			end[i]++
			return end[:i+1]
		}
	}
	return append(key, 0)
}

type RESTServer struct {
	kvClient quorumv1.KVClient
}

func NewRESTServer(kv quorumv1.KVClient) *RESTServer {
	return &RESTServer{kvClient: kv}
}

func (s *RESTServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/containers", s.handleRun)
	mux.HandleFunc("GET /api/v1/containers", s.handlePs)
	mux.HandleFunc("DELETE /api/v1/containers/{name}", s.handleRm)
	mux.HandleFunc("POST /api/v1/containers/{name}/stop", s.handleStop)
	mux.HandleFunc("GET /api/v1/containers/{name}/logs", s.handleLogs)
	mux.HandleFunc("GET /api/v1/nodes", s.handleNodes)
	mux.HandleFunc("POST /api/v1/containers/{name}/scale", s.handleScale)
	return mux
}

func (s *RESTServer) handleRun(w http.ResponseWriter, r *http.Request) {
	var spec ContainerSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if spec.Name == "" || spec.Image == "" {
		writeError(w, http.StatusBadRequest, "name and image are required")
		return
	}
	if spec.Replicas < 1 {
		spec.Replicas = 1
	}

	val, _ := json.Marshal(spec)
	key := fmt.Sprintf("/containers/%s/spec", spec.Name)
	_, err := s.kvClient.Put(r.Context(), &quorumv1.PutRequest{
		Key: []byte(key), Value: val,
	})
	if err != nil {
		if leader, ok := isLeaderUnavailable(err); ok {
			writeLeaderRedirect(w, leader)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"name":     spec.Name,
		"replicas": spec.Replicas,
	})
}

func stripIndex(name string) string {
	if idx := strings.LastIndex(name, "-"); idx >= 0 {
		if _, err := strconv.Atoi(name[idx+1:]); err == nil {
			return name[:idx]
		}
	}
	return name
}

func (s *RESTServer) handlePs(w http.ResponseWriter, r *http.Request) {
	resp, err := s.kvClient.Range(r.Context(), &quorumv1.RangeRequest{
		Key:      []byte("/containers/"),
		RangeEnd: prefixEnd([]byte("/containers/")),
	})
	if err != nil {
		if leader, ok := isLeaderUnavailable(err); ok {
			writeLeaderRedirect(w, leader)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	specs := make(map[string]bool)
	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		if strings.HasSuffix(key, "/spec") {
			name := strings.TrimPrefix(key, "/containers/")
			name = strings.TrimSuffix(name, "/spec")
			specs[name] = true
		}
	}

	statuses := make(map[string]ContainerInfo)
	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		if !strings.HasSuffix(key, "/status") {
			continue
		}
		name := strings.TrimPrefix(key, "/containers/")
		name = strings.TrimSuffix(name, "/status")
		if !specs[stripIndex(name)] {
			continue
		}
		var info ContainerInfo
		if json.Unmarshal(kv.Value, &info) == nil {
			if info.Name == "" {
				info.Name = name
			}
			statuses[name] = info
		}
	}

	names := make(map[string]bool)
	for n := range statuses {
		names[n] = true
	}
	for n := range specs {
		names[n] = true
	}

	var containers []ContainerInfo
	for n := range names {
		if st, ok := statuses[n]; ok {
			containers = append(containers, st)
		} else if !specs[n] {
			continue
		} else {
			hasIndexed := false
			for sn := range statuses {
				if stripIndex(sn) == n {
					hasIndexed = true
					break
				}
			}
			if !hasIndexed {
				containers = append(containers, ContainerInfo{Name: n, Status: "pending"})
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"containers": containers})
}

func (s *RESTServer) handleRm(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing container name")
		return
	}

	s.kvClient.Delete(r.Context(), &quorumv1.DeleteRequest{
		Key: []byte(fmt.Sprintf("/containers/%s/spec", name)),
	})
	s.kvClient.Delete(r.Context(), &quorumv1.DeleteRequest{
		Key: []byte(fmt.Sprintf("/containers/%s/status", name)),
	})
	indexedPref := []byte(fmt.Sprintf("/containers/%s-", name))
	if ir, _ := s.kvClient.Range(r.Context(), &quorumv1.RangeRequest{
		Key: indexedPref, RangeEnd: prefixEnd(indexedPref),
	}); ir != nil {
		for _, kv := range ir.Kvs {
			s.kvClient.Delete(r.Context(), &quorumv1.DeleteRequest{Key: kv.Key})
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

func (s *RESTServer) handleStop(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing container name")
		return
	}

	s.kvClient.Delete(r.Context(), &quorumv1.DeleteRequest{
		Key: []byte(fmt.Sprintf("/containers/%s/spec", name)),
	})
	s.kvClient.Delete(r.Context(), &quorumv1.DeleteRequest{
		Key: []byte(fmt.Sprintf("/containers/%s/status", name)),
	})

	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

func (s *RESTServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing container name")
		return
	}

	prefix := fmt.Sprintf("/containers/%s/logs/", name)
	resp, err := s.kvClient.Range(r.Context(), &quorumv1.RangeRequest{
		Key:      []byte(prefix),
		RangeEnd: prefixEnd([]byte(prefix)),
	})
	if err != nil {
		if leader, ok := isLeaderUnavailable(err); ok {
			writeLeaderRedirect(w, leader)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var entries []LogEntry
	for _, kv := range resp.Kvs {
		entries = append(entries, LogEntry{
			Key:   strings.TrimPrefix(string(kv.Key), prefix),
			Value: string(kv.Value),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"logs": entries})
}

func (s *RESTServer) handleNodes(w http.ResponseWriter, r *http.Request) {
	resp, err := s.kvClient.Range(r.Context(), &quorumv1.RangeRequest{
		Key:      []byte("/nodes/"),
		RangeEnd: prefixEnd([]byte("/nodes/")),
	})
	if err != nil {
		if leader, ok := isLeaderUnavailable(err); ok {
			writeLeaderRedirect(w, leader)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var nodes []NodeInfo
	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		if strings.Contains(key, "/heartbeat") || strings.Contains(key, "/assignments") {
			continue
		}
		var info NodeInfo
		if json.Unmarshal(kv.Value, &info) == nil {
			nodes = append(nodes, info)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
}

func (s *RESTServer) handleScale(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing container name")
		return
	}

	var req ScaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}

	getResp, err := s.kvClient.Get(r.Context(), &quorumv1.GetRequest{
		Key: []byte(fmt.Sprintf("/containers/%s/spec", name)),
	})
	if err != nil {
		if leader, ok := isLeaderUnavailable(err); ok {
			writeLeaderRedirect(w, leader)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !getResp.Found {
		writeError(w, http.StatusNotFound, "container not found")
		return
	}

	var spec ContainerSpec
	if err := json.Unmarshal(getResp.Value, &spec); err != nil {
		writeError(w, http.StatusInternalServerError, "parse spec: "+err.Error())
		return
	}
	spec.Replicas = req.Replicas

	newVal, _ := json.Marshal(spec)
	_, err = s.kvClient.Put(r.Context(), &quorumv1.PutRequest{
		Key:   []byte(fmt.Sprintf("/containers/%s/spec", name)),
		Value: newVal,
	})
	if err != nil {
		if leader, ok := isLeaderUnavailable(err); ok {
			writeLeaderRedirect(w, leader)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name":     name,
		"replicas": spec.Replicas,
	})
}
