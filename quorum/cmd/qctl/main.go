package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
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
		fmt.Fprintf(os.Stderr, "Usage: qctl --addr <addr> <put|get|del|watch> [key] [value]\n")
		os.Exit(1)
	}
	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %s\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	kv := quorumv1.NewKVClient(conn)

	switch args[0] {
	case "put":
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: qctl put <key> <value>")
			os.Exit(1)
		}
		resp, err := kv.Put(ctx, &quorumv1.PutRequest{Key: []byte(args[1]), Value: []byte(args[2])})
		if err != nil {
			handleRedirect(err)
		}
		fmt.Println(resp.Revision)
	case "get":
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		prefix := false
		pos := []string{}
		for _, a := range args[1:] {
			if a == "--prefix" {
				prefix = true
			} else {
				pos = append(pos, a)
			}
		}
		if len(pos) < 1 {
			fmt.Fprintln(os.Stderr, "Usage: qctl get [--prefix] <key>")
			os.Exit(1)
		}
		key := []byte(pos[0])
		var rangeEnd []byte
		if prefix {
			rangeEnd = prefixEnd(key)
		}
		resp, err := kv.Range(ctx, &quorumv1.RangeRequest{Key: key, RangeEnd: rangeEnd})
		if err != nil {
			handleRedirect(err)
		}
		for _, kv := range resp.Kvs {
			fmt.Printf("%s = %s (rev %d)\n", kv.Key, kv.Value, kv.Revision)
		}
		if resp.More {
			fmt.Fprintf(os.Stderr, "(more results available)\n")
		}
		if len(resp.Kvs) == 0 {
			os.Exit(1)
		}
	case "del":
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: qctl del <key>")
			os.Exit(1)
		}
		_, err := kv.Delete(ctx, &quorumv1.DeleteRequest{Key: []byte(args[1])})
		if err != nil {
			handleRedirect(err)
		}
	case "watch":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: qctl watch <key> [--rev N]")
			os.Exit(1)
		}

		key := args[1]
		var startRev int64
		rest := args[2:]
		for i := 0; i < len(rest); i++ {
			if rest[i] == "--rev" && i+1 < len(rest) {
				parsed, err := strconv.ParseInt(rest[i+1], 10, 64)
				if err != nil {
					fmt.Fprintf(os.Stderr, "invalid --rev value: %s\n", rest[i+1])
					os.Exit(1)
				}
				startRev = parsed
				i++
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		go func() {
			<-sigCh
			cancel()
		}()

		wc := quorumv1.NewWatchClient(conn)
		stream, err := wc.Watch(ctx, &quorumv1.WatchRequest{Key: []byte(key), StartRevision: startRev})
		if err != nil {
			handleRedirect(err)
		}
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				if ctx.Err() != nil {
					os.Exit(0)
				}
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
