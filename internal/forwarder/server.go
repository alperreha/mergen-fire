package forwarder

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alperreha/mergen-fire/internal/model"
)

type Dialer interface {
	DialContext(ctx context.Context, network, address, netns string) (net.Conn, error)
}

type Server struct {
	config   Config
	resolver *Resolver
	dialer   Dialer
	logger   *slog.Logger
	cert     tls.Certificate
	connMu   sync.Mutex
	connWG   sync.WaitGroup
	conns    map[net.Conn]struct{}
}

func NewServer(config Config, resolver *Resolver, dialer Dialer, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if resolver == nil {
		return nil, errors.New("resolver is nil")
	}
	if dialer == nil {
		return nil, errors.New("dialer is nil")
	}

	cert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load tls cert/key: %w", err)
	}

	return &Server{
		config:   config,
		resolver: resolver,
		dialer:   dialer,
		logger:   logger,
		cert:     cert,
		conns:    map[net.Conn]struct{}{},
	}, nil
}

func (s *Server) Run(ctx context.Context) error {
	if err := s.runTLSListener(ctx, s.config.HTTPSAddr); err != nil {
		return err
	}
	s.waitForConnections()
	return nil
}

func (s *Server) runTLSListener(ctx context.Context, listenAddr string) error {
	base, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen %s failed: %w", listenAddr, err)
	}
	defer base.Close()

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{s.cert},
		MinVersion:   tls.VersionTLS12,
	}
	listener := tls.NewListener(base, tlsConfig)

	s.logger.Info("forwarder https listener started", "listenAddr", listenAddr)

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if isTemporary(err) {
				s.logger.Warn("temporary accept error", "listenAddr", listenAddr, "error", err)
				time.Sleep(150 * time.Millisecond)
				continue
			}
			return fmt.Errorf("accept failed on %s: %w", listenAddr, err)
		}

		s.trackConn(conn)
		s.connWG.Add(1)
		go s.handleTLSConn(conn)
	}
}

func (s *Server) handleTLSConn(clientConn net.Conn) {
	defer s.connWG.Done()
	defer s.untrackConn(clientConn)
	defer clientConn.Close()

	tlsConn, ok := clientConn.(*tls.Conn)
	if !ok {
		s.logger.Error("unexpected non-tls connection type")
		return
	}

	if err := tlsConn.Handshake(); err != nil {
		s.logger.Warn("tls handshake failed", "remoteAddr", tlsConn.RemoteAddr().String(), "error", err)
		return
	}

	serverName := strings.ToLower(strings.TrimSpace(tlsConn.ConnectionState().ServerName))
	if serverName == "" {
		s.logger.Warn("tls client has no sni")
		_ = writeHTTPError(tlsConn, 421, "missing sni")
		return
	}

	meta, err := s.resolver.Resolve(serverName)
	if err != nil {
		s.logger.Warn("sni resolve failed", "serverName", serverName, "error", err)
		_ = writeHTTPError(tlsConn, 404, "vm not found")
		return
	}

	targetGuestPort, err := targetHTTPPort(meta)
	if err != nil {
		s.logger.Warn("vm http port unavailable", "serverName", serverName, "vmID", meta.ID, "error", err)
		_ = writeHTTPError(tlsConn, 502, "vm http port not configured")
		return
	}

	targetAddr := net.JoinHostPort(meta.GuestIP, strconv.Itoa(targetGuestPort))
	dialCtx, cancel := context.WithTimeout(context.Background(), s.config.DialTimeout)
	defer cancel()

	backendConn, err := s.dialer.DialContext(dialCtx, "tcp", targetAddr, meta.NetNS)
	if err != nil {
		s.logger.Warn(
			"backend dial failed",
			"serverName", serverName,
			"vmID", meta.ID,
			"netns", meta.NetNS,
			"targetAddr", targetAddr,
			"targetGuestPort", targetGuestPort,
			"error", err,
		)
		_ = writeHTTPError(tlsConn, 502, "backend unavailable")
		return
	}
	defer backendConn.Close()

	s.logger.Debug(
		"connection routed",
		"serverName", serverName,
		"vmID", meta.ID,
		"netns", meta.NetNS,
		"targetAddr", targetAddr,
		"targetGuestPort", targetGuestPort,
		"remoteAddr", tlsConn.RemoteAddr().String(),
	)

	proxyStreams(tlsConn, backendConn)
}

func targetHTTPPort(meta model.VMMetadata) (int, error) {
	if meta.HTTPPort <= 0 || meta.HTTPPort > 65535 {
		return 0, fmt.Errorf("invalid httpPort: %d", meta.HTTPPort)
	}
	return meta.HTTPPort, nil
}

func (s *Server) waitForConnections() {
	done := make(chan struct{})
	go func() {
		s.connWG.Wait()
		close(done)
	}()

	timeout := s.config.ShutdownTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	select {
	case <-done:
		s.logger.Info("forwarder graceful shutdown completed")
	case <-time.After(timeout):
		active := s.activeConnectionCount()
		s.logger.Warn(
			"forwarder shutdown timeout reached, forcing connection close",
			"activeConnections", active,
			"timeout", timeout.String(),
		)
		s.closeAllConnections()
		<-done
	}
}

func (s *Server) trackConn(conn net.Conn) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	s.conns[conn] = struct{}{}
}

func (s *Server) untrackConn(conn net.Conn) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	delete(s.conns, conn)
}

func (s *Server) activeConnectionCount() int {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return len(s.conns)
}

func (s *Server) closeAllConnections() {
	s.connMu.Lock()
	snapshot := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		snapshot = append(snapshot, conn)
	}
	s.connMu.Unlock()

	for _, conn := range snapshot {
		_ = conn.Close()
	}
}

func proxyStreams(client net.Conn, backend net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(backend, client)
		if c, ok := backend.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, backend)
		if c, ok := client.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
	}()

	wg.Wait()
}

func writeHTTPError(conn net.Conn, code int, message string) error {
	body := message + "\n"
	response := fmt.Sprintf(
		"HTTP/1.1 %d %s\r\nContent-Type: text/plain\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		code,
		httpStatusText(code),
		len(body),
		body,
	)
	_, err := io.WriteString(conn, response)
	return err
}

func httpStatusText(code int) string {
	switch code {
	case 404:
		return "Not Found"
	case 421:
		return "Misdirected Request"
	case 502:
		return "Bad Gateway"
	default:
		return "Error"
	}
}

func isTemporary(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Temporary()
	}
	return false
}
