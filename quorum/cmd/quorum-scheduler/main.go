package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	quorumv1 "quorum/gen/quorum/v1"
	"quorum/internal/scheduler"
)

func main() {
	dbAddr := flag.String("db-addr", "", "local quorum-db address (e.g. :10101)")
	nodeID := flag.String("id", "", "node id")
	flag.Parse()

	logger := slog.With("component", "quorum-scheduler")

	if *dbAddr == "" || *nodeID == "" {
		fmt.Fprintln(os.Stderr, "Usage: quorum-scheduler --db-addr <addr> --id <id>")
		os.Exit(1)
	}

	conn := connectLocal(*dbAddr, logger)
	if conn == nil {
		os.Exit(1)
	}
	defer conn.Close()

	kv := quorumv1.NewKVClient(conn)
	wc := quorumv1.NewWatchClient(conn)

	nm := scheduler.NewNodeManager(kv, wc, *nodeID)
	sched := scheduler.New(kv, wc, nm, *nodeID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go nm.Run(ctx)
	go sched.Run(ctx)

	logger.Info("scheduler started", "db-addr", conn.Target(), "id", *nodeID)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("shutting down")
	sched.Stop()
	nm.Stop()
}

func connectLocal(addr string, logger *slog.Logger) *grpc.ClientConn {
	logger.Info("connecting to local quorum-db", "addr", addr)
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		logger.Error("connection failed", "addr", addr, "error", err)
		return nil
	}

	logger.Info("connected to local quorum-db")
	return conn
}
