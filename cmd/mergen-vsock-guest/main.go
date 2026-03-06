//go:build linux

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	fvsock "github.com/firecracker-microvm/firecracker-go-sdk/vsock"

	"github.com/alperreha/mergen-fire/internal/vsockcfg"
)

func main() {
	var (
		shellPath     string
		authToken     string
		authTimeout   time.Duration
		oneConnection bool
	)

	flag.StringVar(&shellPath, "shell", "/bin/sh", "Shell binary path")
	flag.StringVar(&authToken, "auth-token", "", "Optional auth token expected as 'AUTH <token>'")
	flag.DurationVar(&authTimeout, "auth-timeout", 3*time.Second, "Authentication handshake timeout")
	flag.BoolVar(&oneConnection, "one-connection", false, "Handle a single connection then exit")
	flag.Parse()
	if strings.TrimSpace(shellPath) == "" {
		die("shell path is required")
	}
	if strings.TrimSpace(authToken) == "" {
		authToken = strings.TrimSpace(os.Getenv("MERGEN_VSOCK_AUTH_TOKEN"))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := fvsock.Listener(ctx, nil, vsockcfg.ShellPort)
	if err != nil {
		die("listen vsock channel %d failed: %v", vsockcfg.ShellPort, err)
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return
			}
			die("accept failed: %v", err)
		}

		if oneConnection {
			handleConn(conn, shellPath, authToken, authTimeout)
			return
		}
		go handleConn(conn, shellPath, authToken, authTimeout)
	}
}

func handleConn(conn io.ReadWriteCloser, shellPath, authToken string, authTimeout time.Duration) {
	defer conn.Close()

	stdin := io.Reader(conn)
	if strings.TrimSpace(authToken) != "" {
		reader := bufio.NewReader(conn)
		if err := authenticate(conn, reader, authToken, authTimeout); err != nil {
			_, _ = io.WriteString(conn, "ERR unauthorized\n")
			return
		}
		stdin = io.MultiReader(reader, conn)
	}

	cmd := exec.Command(shellPath, "-i")
	cmd.Stdin = stdin
	cmd.Stdout = conn
	cmd.Stderr = conn
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		_, _ = io.WriteString(conn, "\n[mergen-vsock-guest] shell terminated: "+err.Error()+"\n")
	}
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

func die(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
