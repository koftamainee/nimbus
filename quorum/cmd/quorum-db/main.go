package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"google.golang.org/grpc"

	quorumv1 "quorum/gen/quorum/v1"
	"quorum/internal/raft"
	"quorum/internal/server"
	"quorum/internal/store"
	"quorum/internal/wal"
)

func main() {
	id := flag.String("id", "", "node id")
	addr := flag.String("addr", "", "listen address (e.g. :9090)")
	initialCluster := flag.String("initial-cluster", "", "comma-separated id=addr pairs")
	dataDir := flag.String("data-dir", "./data", "data directory")
	flag.Parse()

	if *id == "" || *addr == "" || *initialCluster == "" {
		fmt.Fprintf(os.Stderr, "Usage: quorum-db --id <id> --addr <addr> --initial-cluster <id=addr,...> --data-dir <dir>\n")
		os.Exit(1)
	}

	peers, peerAddrs := parseInitialCluster(*id, *initialCluster)

	logger := slog.With("node", *id)

	os.MkdirAll(*dataDir, 0755)
	w, err := wal.Open(filepath.Join(*dataDir, *id+".wal"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "open WAL: %s\n", err)
		os.Exit(1)
	}

	st := store.New()

	transport := raft.NewGRPCTransport()
	for p, a := range peerAddrs {
		transport.SetPeer(p, a)
	}

	r := raft.New(*id, peers, transport, st, w, logger)

	go r.Run()
	<-r.Ready()

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen: %s\n", err)
		os.Exit(1)
	}

	gs := grpc.NewServer()
	quorumv1.RegisterRaftServer(gs, server.NewRaftServer(r))
	quorumv1.RegisterKVServer(gs, server.NewKVServer(r, st))
	quorumv1.RegisterWatchServer(gs, server.NewWatchServer(r, st))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("shutting down")
		gs.GracefulStop()
		w.Close()
		os.Exit(0)
	}()

	logger.Info("listening", "addr", *addr)
	if err := gs.Serve(lis); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %s\n", err)
		os.Exit(1)
	}
}

func parseInitialCluster(myID, cluster string) (peers []string, peerAddrs map[string]string) {
	peerAddrs = make(map[string]string)
	for _, part := range strings.Split(cluster, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.IndexByte(part, '=')
		if eq < 0 {
			continue
		}
		id := part[:eq]
		addr := part[eq+1:]
		if id == myID {
			continue
		}
		peers = append(peers, id)
		peerAddrs[id] = addr
	}
	return
}
