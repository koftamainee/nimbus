package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	quorumv1 "quorum/gen/quorum/v1"
)

type ContainerSpec struct {
	Name     string   `json:"name"`
	Image    string   `json:"image"`
	Memory   string   `json:"memory"`
	Cpus     float64  `json:"cpus"`
	Env      []string `json:"env"`
	Cmd      []string `json:"cmd"`
	Replicas int    `json:"replicas"`
}

type Assignment struct {
	ContainerName string         `json:"container_name"`
	Spec          ContainerSpec `json:"spec"`
	NodeID        string        `json:"node_id"`
	Status        string        `json:"status"`
}

func main() {
	addr := flag.String("addr", "localhost:9090", "quorum address")
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %s\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	kv := quorumv1.NewKVClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch args[0] {
	case "run":
		cmdRun(ctx, kv, args[1:])
	case "ps":
		cmdPs(ctx, kv)
	case "rm":
		cmdRm(ctx, kv, args[1:])
	case "stop":
		cmdStop(ctx, kv, args[1:])
	case "logs":
		cmdLogs(ctx, kv, args[1:])
	case "nodes":
		cmdNodes(ctx, kv)
	case "scale":
		cmdScale(ctx, kv, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: nimbusctl [--addr <addr>] <command> [args]

Commands:
  run <name> --image <img> [--replicas N] [--mem <mb>] [--cpus <n>] [--env K=V]... [--cmd arg]...
  ps
  rm <name>...
  stop <name>...
  logs <name>
  nodes
  scale <name> --replicas N
`)
}

func cmdRun(ctx context.Context, kv quorumv1.KVClient, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: nimbusctl run <name> --image <img> [--replicas N]")
		os.Exit(1)
	}
	name := args[0]
	spec := ContainerSpec{Name: name, Replicas: 1}
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--addr":
			i++ // consumed globally, skip
		case "--image":
			i++
			if i >= len(rest) {
				fmt.Fprintln(os.Stderr, "--image requires a value")
				os.Exit(1)
			}
			spec.Image = rest[i]
		case "--replicas":
			i++
			if i >= len(rest) {
				fmt.Fprintln(os.Stderr, "--replicas requires a value")
				os.Exit(1)
			}
			n, err := fmt.Sscanf(rest[i], "%d", &spec.Replicas)
			if n != 1 || err != nil {
				fmt.Fprintf(os.Stderr, "invalid --replicas value: %s\n", rest[i])
				os.Exit(1)
			}
		case "--mem":
			i++
			if i >= len(rest) {
				fmt.Fprintln(os.Stderr, "--mem requires a value")
				os.Exit(1)
			}
			spec.Memory = rest[i]
		case "--cpus":
			i++
			if i >= len(rest) {
				fmt.Fprintln(os.Stderr, "--cpus requires a value")
				os.Exit(1)
			}
			fmt.Sscanf(rest[i], "%f", &spec.Cpus)
		case "--env":
			i++
			if i >= len(rest) {
				fmt.Fprintln(os.Stderr, "--env requires a value")
				os.Exit(1)
			}
			spec.Env = append(spec.Env, rest[i])
		case "--cmd":
			i++
			if i >= len(rest) {
				fmt.Fprintln(os.Stderr, "--cmd requires a value")
				os.Exit(1)
			}
			spec.Cmd = append(spec.Cmd, rest[i])
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", rest[i])
			os.Exit(1)
		}
	}
	if spec.Image == "" {
		fmt.Fprintln(os.Stderr, "Usage: nimbusctl run <name> --image <img> [--replicas N]")
		os.Exit(1)
	}

	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "NIMBUS_ENV_") {
			spec.Env = append(spec.Env, strings.TrimPrefix(e, "NIMBUS_ENV_"))
		}
	}

	val, _ := json.Marshal(spec)
	key := fmt.Sprintf("/containers/%s/spec", name)
	_, err := kv.Put(ctx, &quorumv1.PutRequest{Key: []byte(key), Value: val})
	handleError(err)
	fmt.Printf("container %s created (%d replicas)\n", name, spec.Replicas)
}

func cmdPs(ctx context.Context, kv quorumv1.KVClient) {
	resp, err := kv.Range(ctx, &quorumv1.RangeRequest{
		Key:      []byte("/containers/"),
		RangeEnd: prefixEnd([]byte("/containers/")),
	})
	if err != nil {
		handleError(err)
		os.Exit(1)
	}

	type info struct {
		NodeID string `json:"node_id"`
		Status string `json:"status"`
	}
	statuses := make(map[string]info)
	specs := make(map[string]bool)
	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		name := strings.TrimPrefix(key, "/containers/")
		if strings.HasSuffix(key, "/status") {
			name = strings.TrimSuffix(name, "/status")
			var st info
			if json.Unmarshal(kv.Value, &st) == nil {
				statuses[name] = st
			}
		} else if strings.HasSuffix(key, "/spec") {
			name = strings.TrimSuffix(name, "/spec")
			specs[name] = true
		}
	}

	names := make(map[string]bool)
	for n := range statuses {
		names[n] = true
	}
	for n := range specs {
		names[n] = true
	}

	fmt.Printf("%-30s %-20s %-12s\n", "NAME", "NODE", "STATUS")
	for n := range names {
		if st, ok := statuses[n]; ok {
			fmt.Printf("%-30s %-20s %-12s\n", n, st.NodeID, st.Status)
		} else {
			fmt.Printf("%-30s %-20s %-12s\n", n, "-", "pending")
		}
	}
}

func cmdRm(ctx context.Context, kv quorumv1.KVClient, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: nimbusctl rm <name>...")
		os.Exit(1)
	}
	for _, name := range args {
		_, err := kv.Delete(ctx, &quorumv1.DeleteRequest{
			Key: []byte(fmt.Sprintf("/containers/%s/spec", name)),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error removing %s: %s\n", name, err)
		} else {
			fmt.Println(name)
		}
	}
}

func cmdStop(ctx context.Context, kv quorumv1.KVClient, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: nimbusctl stop <name>...")
		os.Exit(1)
	}
	for _, name := range args {
		_, err := kv.Delete(ctx, &quorumv1.DeleteRequest{
			Key: []byte(fmt.Sprintf("/containers/%s/spec", name)),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error stopping %s: %s\n", name, err)
		} else {
			fmt.Println(name)
		}
	}
}

func cmdLogs(ctx context.Context, kv quorumv1.KVClient, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: nimbusctl logs <name>")
		os.Exit(1)
	}
	name := args[0]
	resp, err := kv.Range(ctx, &quorumv1.RangeRequest{
		Key:      []byte(fmt.Sprintf("/containers/%s/logs/", name)),
		RangeEnd: prefixEnd([]byte(fmt.Sprintf("/containers/%s/logs/", name))),
	})
	if err != nil {
		handleError(err)
		os.Exit(1)
	}
	for _, kv := range resp.Kvs {
		fmt.Printf("%s: %s\n", kv.Key, kv.Value)
	}
}

func cmdNodes(ctx context.Context, kv quorumv1.KVClient) {
	resp, err := kv.Range(ctx, &quorumv1.RangeRequest{
		Key:      []byte("/nodes/"),
		RangeEnd: prefixEnd([]byte("/nodes/")),
	})
	if err != nil {
		handleError(err)
		os.Exit(1)
	}
	fmt.Printf("%-20s %-20s %-12s\n", "NODE ID", "HOSTNAME", "STATUS")
	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		if strings.Contains(key, "/heartbeat") || strings.Contains(key, "/assignments") {
			continue
		}
		var info struct {
			ID       string `json:"id"`
			Hostname string `json:"hostname"`
		}
		if json.Unmarshal(kv.Value, &info) == nil {
			fmt.Printf("%-20s %-20s\n", info.ID, info.Hostname)
		}
	}
}

func cmdScale(ctx context.Context, kv quorumv1.KVClient, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: nimbusctl scale <name> --replicas N")
		os.Exit(1)
	}
	name := args[0]
	replicas := 1
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--addr":
			i++
		case "--replicas":
			i++
			if i >= len(rest) {
				fmt.Fprintln(os.Stderr, "--replicas requires a value")
				os.Exit(1)
			}
			fmt.Sscanf(rest[i], "%d", &replicas)
		}
	}

	resp, err := kv.Get(ctx, &quorumv1.GetRequest{
		Key: []byte(fmt.Sprintf("/containers/%s/spec", name)),
	})
	if err != nil {
		handleError(err)
		os.Exit(1)
	}
	if !resp.Found {
		fmt.Fprintf(os.Stderr, "container %s not found\n", name)
		os.Exit(1)
	}

	var spec ContainerSpec
	if err := json.Unmarshal(resp.Value, &spec); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing spec: %s\n", err)
		os.Exit(1)
	}
	spec.Replicas = replicas

	val, _ := json.Marshal(spec)
	_, err = kv.Put(ctx, &quorumv1.PutRequest{
		Key:   []byte(fmt.Sprintf("/containers/%s/spec", name)),
		Value: val,
	})
	if err != nil {
		handleError(err)
		os.Exit(1)
	}
	fmt.Printf("scaled %s to %d replicas\n", name, replicas)
}

func handleError(err error) {
	if err == nil {
		return
	}
	st, ok := status.FromError(err)
	if ok && st.Code().String() == "Unavailable" && strings.HasPrefix(st.Message(), "leader:") {
		leader := strings.TrimPrefix(st.Message(), "leader: ")
		fmt.Fprintf(os.Stderr, "redirect to leader: %s\n", leader)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "error: %s\n", err)
	os.Exit(1)
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
