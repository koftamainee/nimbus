package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	quorumv1 "quorum/gen/quorum/v1"
)

func main() {
	addr := flag.String("addr", "localhost:9090", "server address")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: qctl --addr <addr> <put|get|del|txn> [key] [value]\n")
		os.Exit(1)
	}

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %s\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := quorumv1.NewKVClient(conn)

	switch args[0] {
	case "put":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: qctl put <key> <value>")
			os.Exit(1)
		}
		resp, err := client.Put(ctx, &quorumv1.PutRequest{Key: []byte(args[1]), Value: []byte(args[2])})
		if err != nil {
			handleRedirect(err)
		}
		fmt.Println(resp.Revision)

	case "get":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: qctl get <key>")
			os.Exit(1)
		}
		resp, err := client.Get(ctx, &quorumv1.GetRequest{Key: []byte(args[1])})
		if err != nil {
			handleRedirect(err)
		}
		if !resp.Found {
			os.Exit(1)
		}
		fmt.Println(string(resp.Value))

	case "del":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: qctl del <key>")
			os.Exit(1)
		}
		_, err := client.Delete(ctx, &quorumv1.DeleteRequest{Key: []byte(args[1])})
		if err != nil {
			handleRedirect(err)
		}

	case "watch":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: qctl watch <key>")
			os.Exit(1)
		}
		stream, err := client.(quorumv1.WatchClient).Watch(ctx, &quorumv1.WatchRequest{Key: []byte(args[1])})
		if err != nil {
			handleRedirect(err)
		}
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "watch error: %s\n", err)
				os.Exit(1)
			}
			for _, ev := range resp.Events {
				fmt.Printf("%s %s = %s (rev %d)\n", ev.Type, ev.Key, ev.Value, ev.Revision)
			}
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		os.Exit(1)
	}
}

func handleRedirect(err error) {
	st, ok := status.FromError(err)
	if ok && st.Code().String() == "Unavailable" && strings.HasPrefix(st.Message(), "leader:") {
		leader := strings.TrimPrefix(st.Message(), "leader: ")
		fmt.Fprintf(os.Stderr, "redirect to leader: %s\n", leader)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "error: %s\n", err)
	os.Exit(1)
}
