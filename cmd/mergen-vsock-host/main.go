package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	fvsock "github.com/firecracker-microvm/firecracker-go-sdk/vsock"

	"github.com/alperreha/mergen-fire/internal/model"
	"github.com/alperreha/mergen-fire/internal/vsockcfg"
)

const defaultConfigRoot = "/var/lib/mergen/vm.d"

func main() {
	var (
		vmID           string
		vmJSONPath     string
		configRoot     string
		udsPath        string
		authToken      string
		nonInteractive string
		retryTimeout   time.Duration
		retryInterval  time.Duration
	)

	flag.StringVar(&vmID, "vm-id", "", "VM id (used with -config-root to resolve vm.json)")
	flag.StringVar(&vmJSONPath, "vm-json", "", "Path to vm.json (used when -uds-path is empty)")
	flag.StringVar(&configRoot, "config-root", defaultConfigRoot, "Root directory containing VM configs")
	flag.StringVar(&udsPath, "uds-path", "", "Firecracker vsock UDS path (if empty, resolved from vm.json)")
	flag.StringVar(&authToken, "auth-token", "", "Optional auth token sent as 'AUTH <token>'")
	flag.StringVar(&nonInteractive, "command", "", "Optional one-shot command to run and exit")
	flag.DurationVar(&retryTimeout, "retry-timeout", 15*time.Second, "VSock dial retry timeout")
	flag.DurationVar(&retryInterval, "retry-interval", 100*time.Millisecond, "VSock dial retry interval")
	flag.Parse()

	resolvedUDS, err := resolveUDSPath(udsPath, vmJSONPath, vmID, configRoot)
	if err != nil {
		die("resolve uds path: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, err := fvsock.DialContext(
		ctx,
		resolvedUDS,
		vsockcfg.ShellPort,
		fvsock.WithRetryTimeout(retryTimeout),
		fvsock.WithRetryInterval(retryInterval),
	)
	if err != nil {
		die("vsock dial failed: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	if err := performAuth(conn, reader, authToken); err != nil {
		die("auth failed: %v", err)
	}

	if strings.TrimSpace(nonInteractive) != "" {
		if err := runOneShot(conn, reader, nonInteractive); err != nil {
			die("command mode failed: %v", err)
		}
		return
	}

	if err := runInteractive(conn, reader); err != nil {
		die("interactive mode failed: %v", err)
	}
}

func resolveUDSPath(explicit, vmJSONPath, vmID, configRoot string) (string, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		return explicit, nil
	}

	vmJSONPath = strings.TrimSpace(vmJSONPath)
	if vmJSONPath == "" {
		if strings.TrimSpace(vmID) == "" {
			return "", errors.New("either -uds-path or (-vm-id/-vm-json) is required")
		}
		vmJSONPath = filepath.Join(strings.TrimSpace(configRoot), vmID, "vm.json")
	}

	body, err := os.ReadFile(vmJSONPath)
	if err != nil {
		return "", fmt.Errorf("read vm config %s: %w", vmJSONPath, err)
	}

	var cfg model.VMConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return "", fmt.Errorf("decode vm config %s: %w", vmJSONPath, err)
	}
	if cfg.Vsock == nil {
		return "", fmt.Errorf("vm config has no vsock device: %s", vmJSONPath)
	}

	resolved := strings.TrimSpace(model.StringValue(cfg.Vsock.UdsPath))
	if resolved == "" {
		return "", fmt.Errorf("vm config vsock uds_path is empty: %s", vmJSONPath)
	}
	return resolved, nil
}

func performAuth(conn io.Writer, reader *bufio.Reader, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("MERGEN_VSOCK_AUTH_TOKEN"))
	}
	if token == "" {
		return nil
	}

	if _, err := io.WriteString(conn, "AUTH "+token+"\n"); err != nil {
		return fmt.Errorf("write auth line: %w", err)
	}

	ack, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read auth ack: %w", err)
	}
	ack = strings.TrimSpace(ack)
	if ack != "OK" {
		return fmt.Errorf("unexpected auth ack: %s", ack)
	}
	return nil
}

func runInteractive(conn io.ReadWriteCloser, reader *bufio.Reader) error {
	go func() {
		_, _ = io.Copy(conn, os.Stdin)
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}()

	_, err := io.Copy(os.Stdout, io.MultiReader(reader, conn))
	_ = conn.Close()
	return err
}

func runOneShot(conn io.ReadWriteCloser, reader *bufio.Reader, command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	if _, err := io.WriteString(conn, command+"\nexit\n"); err != nil {
		return fmt.Errorf("write command: %w", err)
	}
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
	_, err := io.Copy(os.Stdout, io.MultiReader(reader, conn))
	return err
}

func die(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
