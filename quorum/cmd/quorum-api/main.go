package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	quorumv1 "quorum/gen/quorum/v1"
	"quorum/internal/server"
)

func main() {
	dbCluster := flag.String("db-cluster", "", "comma-separated id=addr pairs for quorum-db")
	restAddr := flag.String("rest-addr", ":9096", "REST API listen address")
	flag.Parse()

	logger := slog.With("component", "quorum-api")

	cluster := parseCluster(*dbCluster)
	conn := connectToLeader(cluster, logger)
	if conn == nil {
		os.Exit(1)
	}
	defer conn.Close()

	kv := quorumv1.NewKVClient(conn)
	restSrv := server.NewRESTServer(kv)

	httpSrv := &http.Server{
		Addr:    *restAddr,
		Handler: restSrv.Handler(),
	}

	go func() {
		logger.Info("REST API server", "addr", *restAddr, "db-addr", conn.Target())
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("REST server", "error", err)
			os.Exit(1)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("shutting down")
	httpSrv.Shutdown(context.Background())
}

func connectToLeader(cluster map[string]string, logger *slog.Logger) *grpc.ClientConn {
	targets := clusterAddrs(cluster)
	for _, target := range targets {
		logger.Info("connecting to quorum-db", "addr", target)
		conn, err := grpc.NewClient(target,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
			grpc.WithTimeout(3*time.Second),
		)
		if err != nil {
			logger.Warn("connection failed", "addr", target, "error", err)
			continue
		}

		kv := quorumv1.NewKVClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err = kv.Get(ctx, &quorumv1.GetRequest{Key: []byte("/internal/health")})
		cancel()

		if err != nil {
			st, ok := status.FromError(err)
			if ok && st.Code() == codes.Unavailable && strings.HasPrefix(st.Message(), "leader: ") {
				leaderID := strings.TrimPrefix(st.Message(), "leader: ")
				logger.Info("redirected to leader", "leader", leaderID)
				if leaderAddr, ok := cluster[leaderID]; ok {
					conn.Close()
					leaderConn, err := grpc.NewClient(leaderAddr,
						grpc.WithTransportCredentials(insecure.NewCredentials()),
						grpc.WithBlock(),
						grpc.WithTimeout(3*time.Second),
					)
					if err != nil {
						logger.Warn("connect to leader failed", "addr", leaderAddr, "error", err)
						continue
					}
					return leaderConn
				}
				return conn
			}
			logger.Warn("health check failed", "addr", target, "error", err)
			conn.Close()
			continue
		}

		return conn
	}

	logger.Error("could not connect to any quorum-db node")
	return nil
}

func clusterAddrs(m map[string]string) []string {
	var addrs []string
	for _, addr := range m {
		addrs = append(addrs, addr)
	}
	return addrs
}

func parseCluster(cluster string) map[string]string {
	result := make(map[string]string)
	if cluster == "" {
		return result
	}
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
		result[id] = addr
	}
	return result
}
