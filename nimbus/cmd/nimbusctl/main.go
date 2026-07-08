package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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

type apiError struct {
	Error  string `json:"error"`
	Leader string `json:"leader,omitempty"`
}

func main() {
	addr := flag.String("addr", "http://localhost:9096", "quorum REST API address")
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	baseURL := strings.TrimRight(*addr, "/")

	switch args[0] {
	case "run":
		cmdRun(baseURL, args[1:])
	case "ps":
		cmdPs(baseURL)
	case "rm":
		cmdRm(baseURL, args[1:])
	case "stop":
		cmdStop(baseURL, args[1:])
	case "nodes":
		cmdNodes(baseURL)
	case "scale":
		cmdScale(baseURL, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: nimbusctl [--addr <url>] <command> [args]

Commands:
  run <name> --image <img> [--replicas N] [--mem <mb>] [--cpus <n>] [--env K=V]... [--cmd arg]...
  ps
  rm <name>...
  stop <name>...
  nodes
  scale <name> [--replicas N]
`)
}

func doRequest(baseURL, method, path string, body any) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	return resp, nil
}

func checkRedirect(resp *http.Response) {
	if resp.StatusCode == http.StatusServiceUnavailable {
		var ae apiError
		if json.NewDecoder(resp.Body).Decode(&ae) == nil && ae.Leader != "" {
			fmt.Fprintf(os.Stderr, "redirect to leader: %s\n", ae.Leader)
			os.Exit(1)
		}
	}
}

func cmdRun(baseURL string, args []string) {
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
			i++
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

	resp, err := doRequest(baseURL, "POST", "/api/v1/containers", spec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	checkRedirect(resp)

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: %s\n", string(body))
		os.Exit(1)
	}

	var result struct {
		Name     string `json:"name"`
		Replicas int    `json:"replicas"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	fmt.Printf("container %s created (%d replicas)\n", result.Name, result.Replicas)
}

func cmdPs(baseURL string) {
	resp, err := doRequest(baseURL, "GET", "/api/v1/containers", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	checkRedirect(resp)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: %s\n", string(body))
		os.Exit(1)
	}

	var result struct {
		Containers []struct {
			Name   string `json:"name"`
			NodeID string `json:"node_id,omitempty"`
			Status string `json:"status,omitempty"`
		} `json:"containers"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("%-30s %-20s %-12s\n", "NAME", "NODE", "STATUS")
	for _, c := range result.Containers {
		node := c.NodeID
		if node == "" {
			node = "-"
		}
		status := c.Status
		if status == "" {
			status = "pending"
		}
		fmt.Printf("%-30s %-20s %-12s\n", c.Name, node, status)
	}
}

func cmdRm(baseURL string, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: nimbusctl rm <name>...")
		os.Exit(1)
	}
	for _, name := range args {
		resp, err := doRequest(baseURL, "DELETE", "/api/v1/containers/"+name, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error removing %s: %s\n", name, err)
			continue
		}
		checkRedirect(resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "error removing %s: status %d\n", name, resp.StatusCode)
			continue
		}
		fmt.Println(name)
	}
}

func cmdStop(baseURL string, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: nimbusctl stop <name>...")
		os.Exit(1)
	}
	for _, name := range args {
		resp, err := doRequest(baseURL, "POST", "/api/v1/containers/"+name+"/stop", nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error stopping %s: %s\n", name, err)
			continue
		}
		checkRedirect(resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "error stopping %s: status %d\n", name, resp.StatusCode)
			continue
		}
		fmt.Println(name)
	}
}

func cmdNodes(baseURL string) {
	resp, err := doRequest(baseURL, "GET", "/api/v1/nodes", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	checkRedirect(resp)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: %s\n", string(body))
		os.Exit(1)
	}

	var result struct {
		Nodes []struct {
			ID       string   `json:"id"`
			Hostname string   `json:"hostname"`
			Addr     string   `json:"addr"`
			Labels   []string `json:"labels,omitempty"`
		} `json:"nodes"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("%-20s %-20s\n", "NODE ID", "HOSTNAME")
	for _, n := range result.Nodes {
		fmt.Printf("%-20s %-20s\n", n.ID, n.Hostname)
	}
}

func cmdScale(baseURL string, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: nimbusctl scale <name> [--replicas N]")
		os.Exit(1)
	}
	name := args[0]
	replicas := 1
	rest := args[1:]
	if len(rest) > 0 && rest[0][0] != '-' {
		fmt.Sscanf(rest[0], "%d", &replicas)
		rest = rest[1:]
	}
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

	body := map[string]int{"replicas": replicas}
	resp, err := doRequest(baseURL, "POST", "/api/v1/containers/"+name+"/scale", body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	checkRedirect(resp)

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: %s\n", string(respBody))
		os.Exit(1)
	}

	var result struct {
		Name     string `json:"name"`
		Replicas int    `json:"replicas"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	fmt.Printf("scaled %s to %d replicas\n", result.Name, result.Replicas)
}
