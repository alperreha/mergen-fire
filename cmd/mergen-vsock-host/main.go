package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
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
		dialTimeout    time.Duration
		retryTimeout   time.Duration
		retryInterval  time.Duration
		commandTimeout time.Duration
		debug          bool
	)

	flag.StringVar(&vmID, "vm-id", "", "VM id (used with -config-root to resolve vm.json)")
	flag.StringVar(&vmJSONPath, "vm-json", "", "Path to vm.json (used when -uds-path is empty)")
	flag.StringVar(&configRoot, "config-root", defaultConfigRoot, "Root directory containing VM configs")
	flag.StringVar(&udsPath, "uds-path", "", "Firecracker vsock UDS path (if empty, resolved from vm.json)")
	flag.StringVar(&authToken, "auth-token", "", "Optional auth token sent as 'AUTH <token>'")
	flag.StringVar(&nonInteractive, "command", "", "Optional one-shot command to run and exit")
	flag.DurationVar(&dialTimeout, "dial-timeout", 15*time.Second, "Total dial timeout for vsock handshake (0 disables deadline)")
	flag.DurationVar(&retryTimeout, "retry-timeout", 15*time.Second, "VSock dial retry timeout")
	flag.DurationVar(&retryInterval, "retry-interval", 100*time.Millisecond, "VSock dial retry interval")
	flag.DurationVar(&commandTimeout, "command-timeout", 30*time.Second, "One-shot command timeout")
	flag.BoolVar(&debug, "debug", false, "Enable debug logs")
	flag.Parse()
	if !debug {
		debug = boolFromEnv(vsockcfg.DebugEnvVar)
	}

	debugf(debug, "start vmID=%q vmJSON=%q configRoot=%q udsPath=%q commandMode=%t", vmID, vmJSONPath, configRoot, udsPath, strings.TrimSpace(nonInteractive) != "")

	resolvedUDS, err := resolveUDSPath(udsPath, vmJSONPath, vmID, configRoot)
	if err != nil {
		die("resolve uds path: %v", err)
	}
	debugf(debug, "resolved vsock uds path: %s", resolvedUDS)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	debugf(debug, "dialing vsock channel=%d dialTimeout=%s retryTimeout=%s retryInterval=%s", vsockcfg.ShellPort, dialTimeout, retryTimeout, retryInterval)
	dialCtx := ctx
	cancelDial := func() {}
	if dialTimeout > 0 {
		dialCtxWithTimeout, cancel := context.WithTimeout(ctx, dialTimeout)
		dialCtx = dialCtxWithTimeout
		cancelDial = cancel
	}
	defer cancelDial()

	conn, err := fvsock.DialContext(
		dialCtx,
		resolvedUDS,
		vsockcfg.ShellPort,
		fvsock.WithRetryTimeout(retryTimeout),
		fvsock.WithRetryInterval(retryInterval),
	)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			die("vsock dial timeout after %s (uds=%s, channel=%d). guest listener may be down or vsock guest not started", dialTimeout, resolvedUDS, vsockcfg.ShellPort)
		}
		die("vsock dial failed: %v", err)
	}
	defer conn.Close()
	debugf(debug, "vsock dial successful")

	reader := bufio.NewReader(conn)
	if err := performAuth(conn, reader, authToken, debug); err != nil {
		die("auth failed: %v", err)
	}

	if strings.TrimSpace(nonInteractive) != "" {
		if err := runOneShot(conn, reader, nonInteractive, commandTimeout, debug); err != nil {
			die("command mode failed: %v", err)
		}
		return
	}

	if err := runInteractive(conn, reader, debug); err != nil {
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

func performAuth(conn io.Writer, reader *bufio.Reader, token string, debug bool) error {
	token = strings.TrimSpace(token)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("MERGEN_VSOCK_AUTH_TOKEN"))
	}
	if token == "" {
		debugf(debug, "auth disabled")
		return nil
	}

	debugf(debug, "sending auth line")
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
	debugf(debug, "auth acknowledged")
	return nil
}

func runInteractive(conn io.ReadWriteCloser, reader *bufio.Reader, debug bool) error {
	debugf(debug, "entering interactive mode")
	go func() {
		_, _ = io.Copy(conn, os.Stdin)
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}()

	_, err := io.Copy(os.Stdout, io.MultiReader(reader, conn))
	_ = conn.Close()
	debugf(debug, "interactive session ended err=%v", err)
	return err
}

func runOneShot(conn io.ReadWriteCloser, reader *bufio.Reader, command string, timeout time.Duration, debug bool) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}

	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if deadlineConn, ok := conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = deadlineConn.SetReadDeadline(time.Now().Add(timeout))
		defer deadlineConn.SetReadDeadline(time.Time{})
	}

	debugf(debug, "sending one-shot command frame timeout=%s command=%q", timeout, command)
	frame := vsockcfg.ExecBeginMarker + "\n" + command + "\n" + vsockcfg.ExecEndMarker + "\n"
	if _, err := io.WriteString(conn, frame); err != nil {
		return fmt.Errorf("write one-shot frame: %w", err)
	}

	exitCode := 0
	linesRead := 0
	eofBeforeDone := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				if line != "" {
					linesRead++
					trimmed := strings.TrimRight(line, "\r\n")
					if strings.HasPrefix(trimmed, vsockcfg.ExecDonePrefix) {
						codeRaw := strings.TrimSpace(strings.TrimPrefix(trimmed, vsockcfg.ExecDonePrefix))
						parsed, parseErr := strconv.Atoi(codeRaw)
						if parseErr != nil {
							return fmt.Errorf("invalid done marker %q: %w", trimmed, parseErr)
						}
						exitCode = parsed
						break
					}
					if strings.HasPrefix(trimmed, "ERR ") {
						return fmt.Errorf("guest error: %s", strings.TrimSpace(trimmed))
					}
					_, _ = fmt.Fprint(os.Stdout, line)
				}
				eofBeforeDone = true
				break
			}
			return fmt.Errorf("read command output: %w", err)
		}
		linesRead++
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, vsockcfg.ExecDonePrefix) {
			codeRaw := strings.TrimSpace(strings.TrimPrefix(trimmed, vsockcfg.ExecDonePrefix))
			parsed, parseErr := strconv.Atoi(codeRaw)
			if parseErr != nil {
				return fmt.Errorf("invalid done marker %q: %w", trimmed, parseErr)
			}
			exitCode = parsed
			break
		}
		if strings.HasPrefix(trimmed, "ERR ") {
			return fmt.Errorf("guest error: %s", strings.TrimSpace(trimmed))
		}
		_, _ = fmt.Fprint(os.Stdout, line)
	}

	if eofBeforeDone {
		return fmt.Errorf("connection closed before done marker (lines_read=%d)", linesRead)
	}

	debugf(debug, "one-shot command completed exitCode=%d", exitCode)
	if exitCode != 0 {
		return fmt.Errorf("remote command exited with code %d", exitCode)
	}
	return nil
}

func die(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func debugf(enabled bool, format string, args ...any) {
	if !enabled {
		return
	}
	log.Printf("[mergen-vsock-host] "+format, args...)
}

func boolFromEnv(key string) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
