//go:build linux

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	fvsock "github.com/firecracker-microvm/firecracker-go-sdk/vsock"

	"github.com/alperreha/mergen-fire/pkg/vsockcfg"
)

func main() {
	var (
		shellPath     string
		authToken     string
		authTimeout   time.Duration
		oneConnection bool
		probeTimeout  time.Duration
		debug         bool
	)

	flag.StringVar(&shellPath, "shell", "/bin/sh", "Shell binary path")
	flag.StringVar(&authToken, "auth-token", "", "Optional auth token expected as 'AUTH <token>'")
	flag.DurationVar(&authTimeout, "auth-timeout", 3*time.Second, "Authentication handshake timeout")
	flag.BoolVar(&oneConnection, "one-connection", false, "Handle a single connection then exit")
	flag.DurationVar(&probeTimeout, "probe-timeout", 500*time.Millisecond, "Probe timeout for one-shot frame detection")
	flag.BoolVar(&debug, "debug", false, "Enable debug logs")
	flag.Parse()
	if strings.TrimSpace(shellPath) == "" {
		die("shell path is required")
	}
	if strings.TrimSpace(authToken) == "" {
		authToken = strings.TrimSpace(os.Getenv("MERGEN_VSOCK_AUTH_TOKEN"))
	}
	if !debug {
		debug = boolFromEnv(vsockcfg.DebugEnvVar)
	}
	debugf(debug, "start shell=%q authEnabled=%t channel=%d", shellPath, strings.TrimSpace(authToken) != "", vsockcfg.ShellPort)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := fvsock.Listener(ctx, nil, vsockcfg.ShellPort)
	if err != nil {
		die("listen vsock channel %d failed: %v", vsockcfg.ShellPort, err)
	}
	defer listener.Close()
	debugf(debug, "listener started")

	var connSeq atomic.Uint64

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				debugf(debug, "listener stopping: context cancelled")
				return
			}
			die("accept failed: %v", err)
		}
		connID := connSeq.Add(1)
		debugf(debug, "accepted connection id=%d", connID)

		if oneConnection {
			handleConn(conn, connID, shellPath, authToken, authTimeout, probeTimeout, debug)
			return
		}
		go handleConn(conn, connID, shellPath, authToken, authTimeout, probeTimeout, debug)
	}
}

func handleConn(conn io.ReadWriteCloser, connID uint64, shellPath, authToken string, authTimeout, probeTimeout time.Duration, debug bool) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	if strings.TrimSpace(authToken) != "" {
		if err := authenticate(conn, reader, authToken, authTimeout); err != nil {
			debugf(debug, "connection id=%d auth failed: %v", connID, err)
			_, _ = io.WriteString(conn, "ERR unauthorized\n")
			return
		}
		debugf(debug, "connection id=%d auth success", connID)
	} else {
		debugf(debug, "connection id=%d auth disabled", connID)
	}

	line, gotLine, err := readFirstLineWithTimeout(conn, reader, probeTimeout)
	if err != nil {
		debugf(debug, "connection id=%d first-line probe failed: %v", connID, err)
		_, _ = io.WriteString(conn, "ERR read request\n")
		return
	}

	if gotLine {
		firstLine := strings.TrimRight(line, "\r\n")
		debugf(debug, "connection id=%d first line=%q", connID, firstLine)
		if firstLine == vsockcfg.ExecBeginMarker {
			command, err := readExecCommandFrame(reader)
			if err != nil {
				debugf(debug, "connection id=%d read command frame failed: %v", connID, err)
				_, _ = io.WriteString(conn, "ERR invalid command frame\n")
				return
			}
			debugf(debug, "connection id=%d command frame received command=%q", connID, command)
			exitCode, execErr := runOneShotCommand(conn, shellPath, command)
			if execErr != nil {
				debugf(debug, "connection id=%d one-shot execution error: %v", connID, execErr)
			}
			if _, err := io.WriteString(conn, "\n"+vsockcfg.ExecDonePrefix+strconv.Itoa(exitCode)+"\n"); err != nil {
				debugf(debug, "connection id=%d write done marker failed: %v", connID, err)
			} else {
				debugf(debug, "connection id=%d one-shot completed exitCode=%d", connID, exitCode)
			}
			return
		}
	}

	stdin := io.Reader(io.MultiReader(reader, conn))
	if gotLine {
		stdin = io.MultiReader(strings.NewReader(line), reader, conn)
	}
	debugf(debug, "connection id=%d entering interactive shell mode", connID)
	cmd := exec.Command(shellPath, "-i")
	cmd.Stdin = stdin
	cmd.Stdout = conn
	cmd.Stderr = conn
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		debugf(debug, "connection id=%d interactive shell exited with error: %v", connID, err)
		_, _ = io.WriteString(conn, "\n[mergen-vsock-guest] shell terminated: "+err.Error()+"\n")
		return
	}
	debugf(debug, "connection id=%d interactive shell ended cleanly", connID)
}

func authenticate(conn io.ReadWriteCloser, reader *bufio.Reader, token string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if deadlineConn, ok := conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = deadlineConn.SetReadDeadline(time.Now().Add(timeout))
		defer deadlineConn.SetReadDeadline(time.Time{})
	}

	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	if strings.TrimSpace(line) != "AUTH "+token {
		return errors.New("invalid auth token")
	}
	_, err = io.WriteString(conn, "OK\n")
	return err
}

func readFirstLineWithTimeout(conn io.ReadWriteCloser, reader *bufio.Reader, timeout time.Duration) (string, bool, error) {
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}

	if deadlineConn, ok := conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = deadlineConn.SetReadDeadline(time.Now().Add(timeout))
		defer deadlineConn.SetReadDeadline(time.Time{})
	}

	line, err := reader.ReadString('\n')
	if err != nil {
		if isTimeoutErr(err) {
			return "", false, nil
		}
		if errors.Is(err, io.EOF) {
			if line == "" {
				return "", false, nil
			}
			return line, true, nil
		}
		return "", false, err
	}
	return line, true, nil
}

func readExecCommandFrame(reader *bufio.Reader) (string, error) {
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == vsockcfg.ExecEndMarker {
			break
		}
		lines = append(lines, line)
	}

	command := strings.TrimSpace(strings.Join(lines, ""))
	if command == "" {
		return "", errors.New("empty command")
	}
	return command, nil
}

func runOneShotCommand(conn io.Writer, shellPath, command string) (int, error) {
	cmd := exec.Command(shellPath, "-lc", command)
	cmd.Stdout = conn
	cmd.Stderr = conn
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 127, err
	}
	return 0, nil
}

func isTimeoutErr(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func die(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func debugf(enabled bool, format string, args ...any) {
	if !enabled {
		return
	}
	log.Printf("[mergen-vsock-guest] "+format, args...)
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
