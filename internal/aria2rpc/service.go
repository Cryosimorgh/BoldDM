package aria2rpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type ServiceConfig struct {
	Mode       string
	Executable string
	RPCURL     string
	RPCSecret  string
	StateDir   string
}

type Service struct {
	client  *Client
	cmd     *exec.Cmd
	managed bool
	version Version
}

func OpenService(cfg ServiceConfig) (*Service, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = "auto"
	}
	if mode == "off" {
		return nil, errors.New("aria2 integration is disabled")
	}
	if mode != "auto" && mode != "external" {
		return nil, fmt.Errorf("invalid aria2 mode %q", cfg.Mode)
	}

	if strings.TrimSpace(cfg.RPCURL) != "" {
		client := New(cfg.RPCURL, cfg.RPCSecret)
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		version, err := client.Version(ctx)
		cancel()
		if err == nil {
			return &Service{client: client, version: version}, nil
		}
		if mode == "external" {
			return nil, fmt.Errorf("connect to external aria2 at %s: %w", client.Endpoint(), err)
		}
	}
	if mode == "external" {
		return nil, errors.New("aria2 external mode requires rpcUrl")
	}

	executable, err := findExecutable(cfg.Executable)
	if err != nil {
		return nil, err
	}
	stateDir := strings.TrimSpace(cfg.StateDir)
	if stateDir == "" {
		stateDir = os.TempDir()
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("create aria2 state directory: %w", err)
	}
	sessionPath := filepath.Join(stateDir, "aria2.session")
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		if err := os.WriteFile(sessionPath, nil, 0o600); err != nil {
			return nil, fmt.Errorf("create aria2 session: %w", err)
		}
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	secret, err := randomSecret()
	if err != nil {
		return nil, err
	}
	args := []string{
		"--enable-rpc=true",
		"--rpc-listen-all=false",
		"--rpc-listen-port=" + strconv.Itoa(port),
		"--rpc-secret=" + secret,
		"--input-file=" + sessionPath,
		"--save-session=" + sessionPath,
		"--save-session-interval=5",
		"--continue=true",
		"--max-concurrent-downloads=64",
		"--summary-interval=0",
		"--console-log-level=warn",
		"--enable-color=false",
	}
	cmd := exec.Command(executable, args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start aria2: %w", err)
	}
	client := New(fmt.Sprintf("http://127.0.0.1:%d/jsonrpc", port), secret)
	deadline := time.Now().Add(4 * time.Second)
	var version Version
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		version, lastErr = client.Version(ctx)
		cancel()
		if lastErr == nil {
			return &Service{client: client, cmd: cmd, managed: true, version: version}, nil
		}
		time.Sleep(80 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return nil, fmt.Errorf("aria2 did not become ready: %w", lastErr)
}

func (s *Service) Client() *Client { return s.client }
func (s *Service) Version() Version { return s.version }
func (s *Service) Managed() bool { return s.managed }

func (s *Service) Close() error {
	if s == nil || !s.managed || s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = s.client.Shutdown(ctx)
	cancel()
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
		return nil
	}
}

func findExecutable(configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		if p, err := exec.LookPath(configured); err == nil {
			return p, nil
		}
		if st, err := os.Stat(configured); err == nil && !st.IsDir() {
			return configured, nil
		}
		return "", fmt.Errorf("configured aria2 executable not found: %s", configured)
	}
	name := "aria2c"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("aria2c not found; place %s next to BoltDM, add it to PATH, or configure aria2.executable", name)
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve aria2 RPC port: %w", err)
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("unexpected listener address for aria2 RPC")
	}
	return addr.Port, nil
}

func randomSecret() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate aria2 RPC secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}
