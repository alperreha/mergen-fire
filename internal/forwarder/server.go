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
	}, nil
}

func (s *Server) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(s.config.Listeners))

	for _, listener := range s.config.Listeners {
		listener := listener
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.runListener(ctx, listener); err != nil {
				errCh <- err
			}
		}()
	}

	select {
	case <-ctx.Done():
		wg.Wait()
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (s *Server) runListener(ctx context.Context, listenerCfg Listener) error {
	base, err := net.Listen("tcp", listenerCfg.Addr)
	if err != nil {
		return fmt.Errorf("listen %s failed: %w", listenerCfg.Addr, err)
	}
	defer base.Close()

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{s.cert},
		MinVersion:   tls.VersionTLS12,
	}
	listener := tls.NewListener(base, tlsConfig)

	s.logger.Info("forwarder listener started", "listenAddr", listenerCfg.Addr, "guestPort", listenerCfg.GuestPort)

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
				s.logger.Warn("temporary accept error", "listenAddr", listenerCfg.Addr, "error", err)
				time.Sleep(150 * time.Millisecond)
				continue
			}
			return fmt.Errorf("accept failed on %s: %w", listenerCfg.Addr, err)
		}

		go s.handleConn(conn, listenerCfg)
	}
}

func (s *Server) handleConn(clientConn net.Conn, listenerCfg Listener) {
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

	targetAddr := net.JoinHostPort(meta.GuestIP, strconv.Itoa(listenerCfg.GuestPort))
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
		"remoteAddr", tlsConn.RemoteAddr().String(),
	)

	proxyStreams(tlsConn, backendConn)
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
