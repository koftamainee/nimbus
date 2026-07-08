package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

type MasterConfig struct {
	ID      string `yaml:"id"`
	DBAddr  string `yaml:"db_addr"`
	APIAddr string `yaml:"api_addr"`
}

type RegistryConfig struct {
	Addr string `yaml:"addr"`
}

type ForgedConfig struct {
	Socket       string `yaml:"socket"`
	RegistryAddr string `yaml:"registry_addr"`
}

type Config struct {
	BinaryDir   string          `yaml:"binary_dir"`
	DataDir     string          `yaml:"data_dir"`
	LogDir      string          `yaml:"log_dir"`
	Masters     []MasterConfig  `yaml:"masters"`
	Workers     []string        `yaml:"workers,omitempty"`
	WorkerCount int             `yaml:"worker_count,omitempty"`
	Registry    *RegistryConfig `yaml:"registry,omitempty"`
	Forged      *ForgedConfig   `yaml:"forged,omitempty"`
}

type proc struct {
	name    string
	cmd     *exec.Cmd
	pidFile string
}

func main() {
	cfgPath := flag.String("f", "nimbus.yaml", "config file path")
	flag.Parse()

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %s\n", err)
		os.Exit(1)
	}

	if cfg.BinaryDir == "" {
		self, err := os.Executable()
		if err == nil {
			cfg.BinaryDir = filepath.Dir(self)
		} else {
			cfg.BinaryDir = "./build"
		}
	}
	cfg.BinaryDir, _ = filepath.Abs(cfg.BinaryDir)

	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
	}
	cfg.DataDir, _ = filepath.Abs(cfg.DataDir)

	if cfg.LogDir == "" {
		cfg.LogDir = filepath.Join(cfg.DataDir, "logs")
	}
	cfg.LogDir, _ = filepath.Abs(cfg.LogDir)

	if err := validateBinaries(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "binary check: %s\n", err)
		os.Exit(1)
	}

	cmdStart(cfg)
}

func loadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}

	if len(cfg.Workers) == 0 && cfg.WorkerCount > 0 {
		for i := range cfg.WorkerCount {
			cfg.Workers = append(cfg.Workers, fmt.Sprintf("worker%d", i+1))
		}
	}

	if len(cfg.Masters) == 0 {
		return nil, errors.New("at least one master is required")
	}
	if len(cfg.Workers) == 0 {
		return nil, errors.New("at least one worker is required")
	}

	return &cfg, nil
}

func validateBinaries(cfg *Config) error {
	required := []string{"quorum-db", "quorum-api", "quorum-scheduler", "nimbus-registry", "forged", "forge-agent"}
	for _, name := range required {
		path := filepath.Join(cfg.BinaryDir, name)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%s not found in %s", name, cfg.BinaryDir)
		}
	}
	return nil
}

func dbCluster(cfg *Config) string {
	var parts []string
	for _, m := range cfg.Masters {
		parts = append(parts, m.ID+"="+m.DBAddr)
	}
	return strings.Join(parts, ",")
}

func ensureDirs(cfg *Config) {
	os.MkdirAll(filepath.Join(cfg.DataDir, "run"), 0755)
	os.MkdirAll(cfg.LogDir, 0755)
	for _, m := range cfg.Masters {
		os.MkdirAll(filepath.Join(cfg.DataDir, m.ID), 0755)
	}
	os.MkdirAll(filepath.Join(cfg.DataDir, "images"), 0755)
}

func binPath(cfg *Config, name string) string {
	return filepath.Join(cfg.BinaryDir, name)
}

func pidPath(cfg *Config, name string) string {
	return filepath.Join(cfg.DataDir, "run", name+".pid")
}

func logPath(cfg *Config, name string) string {
	return filepath.Join(cfg.LogDir, name+".log")
}

func startProc(logger *slog.Logger, cfg *Config, binary, name string, args []string, pidFile string) *proc {
	logPath := logPath(cfg, name)
	f, err := os.Create(logPath)
	if err != nil {
		logger.Error("cannot create log file", "name", name, "error", err)
		return nil
	}

	cmd := exec.Command(binPath(cfg, binary), args...)
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		logger.Error("start failed", "name", name, "error", err)
		f.Close()
		return nil
	}

	os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0644)

	logger.Info("started", "name", name, "pid", cmd.Process.Pid, "log", logPath)
	return &proc{name: name, cmd: cmd, pidFile: pidFile}
}

func checkPidFiles(cfg *Config) error {
	check := func(path string) error {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("pid file exists: %s (cluster may be running)", path)
		}
		return nil
	}

	for _, m := range cfg.Masters {
		if err := check(pidPath(cfg, "quorum-db-"+m.ID)); err != nil {
			return err
		}
		if err := check(pidPath(cfg, "quorum-api-"+m.ID)); err != nil {
			return err
		}
		if err := check(pidPath(cfg, "quorum-scheduler-"+m.ID)); err != nil {
			return err
		}
	}
	if cfg.Registry != nil {
		if err := check(pidPath(cfg, "nimbus-registry")); err != nil {
			return err
		}
	}
	if cfg.Forged != nil {
		if err := check(pidPath(cfg, "forged")); err != nil {
			return err
		}
	}
	for _, w := range cfg.Workers {
		if err := check(pidPath(cfg, "forge-agent-"+w)); err != nil {
			return err
		}
	}
	return nil
}

func cmdStart(cfg *Config) {
	logger := slog.With("component", "nimbusadm")

	if err := checkPidFiles(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		fmt.Fprintln(os.Stderr, "Run 'nimbusadm stop' first or remove pid files manually.")
		os.Exit(1)
	}

	ensureDirs(cfg)

	cluster := dbCluster(cfg)
	var procs []*proc
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	for _, m := range cfg.Masters {
		name := "quorum-db-" + m.ID
		p := startProc(logger, cfg, "quorum-db", name, []string{
			"--id", m.ID,
			"--addr", m.DBAddr,
			"--initial-cluster", cluster,
			"--data-dir", filepath.Join(cfg.DataDir, m.ID),
		}, pidPath(cfg, name))
		if p != nil {
			procs = append(procs, p)
		}
	}

	time.Sleep(2 * time.Second)

	for _, m := range cfg.Masters {
		name := "quorum-api-" + m.ID
		p := startProc(logger, cfg, "quorum-api", name, []string{
			"--db-cluster", cluster,
			"--rest-addr", m.APIAddr,
		}, pidPath(cfg, name))
		if p != nil {
			procs = append(procs, p)
		}
	}

	for _, m := range cfg.Masters {
		name := "quorum-scheduler-" + m.ID
		p := startProc(logger, cfg, "quorum-scheduler", name, []string{
			"--db-addr", m.DBAddr,
			"--id", m.ID,
		}, pidPath(cfg, name))
		if p != nil {
			procs = append(procs, p)
		}
	}

	if cfg.Registry != nil {
		p := startProc(logger, cfg, "nimbus-registry", "nimbus-registry", []string{
			"--addr", cfg.Registry.Addr,
			"--image-dir", filepath.Join(cfg.DataDir, "images"),
		}, pidPath(cfg, "nimbus-registry"))
		if p != nil {
			procs = append(procs, p)
		}
	}

	if cfg.Forged != nil {
		p := startProc(logger, cfg, "forged", "forged", []string{
			"--data-dir", cfg.DataDir,
			"--registry-addr", cfg.Forged.RegistryAddr,
		}, pidPath(cfg, "forged"))
		if p != nil {
			procs = append(procs, p)
		}
	}

	for _, w := range cfg.Workers {
		name := "forge-agent-" + w
		p := startProc(logger, cfg, "forge-agent", name, []string{
			"--node-id", w,
			"--db-cluster", cluster,
		}, pidPath(cfg, name))
		if p != nil {
			procs = append(procs, p)
		}
	}

	logger.Info("all processes started", "count", len(procs))
	fmt.Printf("Cluster running: %d masters, %d workers\n", len(cfg.Masters), len(cfg.Workers))
	fmt.Println("Press Ctrl+C to stop.")

	<-sigCh
	logger.Info("shutting down...")
	stopProcs(logger, procs)
}

func stopProcs(logger *slog.Logger, procs []*proc) {
	for i := len(procs) - 1; i >= 0; i-- {
		p := procs[i]
		if p.cmd == nil || p.cmd.Process == nil {
			continue
		}
		logger.Info("stopping", "name", p.name)
		syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
	}

	done := make(chan struct{})
	go func() {
		for _, p := range procs {
			if p.cmd != nil {
				p.cmd.Wait()
			}
		}
		close(done)
	}()

	select {
	case <-done:
		logger.Info("all processes exited gracefully")
	case <-time.After(10 * time.Second):
		logger.Warn("timeout, sending SIGKILL")
		for _, p := range procs {
			if p.cmd != nil && p.cmd.Process != nil {
				syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			}
		}
	}

	for _, p := range procs {
		os.Remove(p.pidFile)
	}
}
