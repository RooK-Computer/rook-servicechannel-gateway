package sshbridge

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"rook-servicechannel-gateway/internal/config"
)

const (
	defaultTerm    = "xterm-256color"
	defaultRows    = 24
	defaultColumns = 80
)

type Client struct {
	username              string
	port                  int
	connectTimeout        time.Duration
	privateKeyPath        string
	insecureIgnoreHostKey bool
}

type remoteSession struct {
	client    *ssh.Client
	session   *ssh.Session
	reader    *io.PipeReader
	writer    *io.PipeWriter
	stdin     io.WriteCloser
	closeOnce sync.Once
}

func NewClient(cfg config.Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Client{
		username:              cfg.SSH.Username,
		port:                  cfg.SSH.Port,
		connectTimeout:        cfg.SSH.ConnectTimeout,
		privateKeyPath:        cfg.Secrets.SSHPrivateKeyPath,
		insecureIgnoreHostKey: cfg.SSH.InsecureIgnoreHostKey,
	}, nil
}

func (c *Client) Open(ctx context.Context, request SessionRequest) (Session, error) {
	hostKeyCallback, err := c.hostKeyCallback()
	if err != nil {
		return nil, err
	}

	privateKey, err := os.ReadFile(c.privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read ssh private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("parse ssh private key: %w", err)
	}

	username := request.Account
	if username == "" {
		username = c.username
	}
	if request.Rows <= 0 {
		request.Rows = defaultRows
	}
	if request.Columns <= 0 {
		request.Columns = defaultColumns
	}
	if request.Term == "" {
		request.Term = defaultTerm
	}

	dialer := net.Dialer{Timeout: c.connectTimeout}
	netConn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(request.IPAddress, fmt.Sprintf("%d", c.port)))
	if err != nil {
		return nil, fmt.Errorf("dial ssh target: %w", err)
	}

	conn, chans, reqs, err := ssh.NewClientConn(netConn, net.JoinHostPort(request.IPAddress, fmt.Sprintf("%d", c.port)), &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         c.connectTimeout,
	})
	if err != nil {
		_ = netConn.Close()
		return nil, fmt.Errorf("establish ssh client connection: %w", err)
	}

	client := ssh.NewClient(conn, chans, reqs)
	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("create ssh session: %w", err)
	}

	reader, writer := io.Pipe()
	session.Stdout = writer
	session.Stderr = writer
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, fmt.Errorf("open ssh stdin pipe: %w", err)
	}

	_ = session.Setenv("LANG", "C.UTF-8")
	_ = session.Setenv("LC_ALL", "C.UTF-8")

	if err := session.RequestPty(request.Term, request.Rows, request.Columns, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		_ = stdin.Close()
		_ = session.Close()
		_ = client.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}

	if err := session.Shell(); err != nil {
		_ = stdin.Close()
		_ = session.Close()
		_ = client.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}

	return &remoteSession{client: client, session: session, reader: reader, writer: writer, stdin: stdin}, nil
}

func (s *remoteSession) Read(buffer []byte) (int, error) {
	return s.reader.Read(buffer)
}

func (s *remoteSession) Write(buffer []byte) (int, error) {
	return s.stdin.Write(buffer)
}

func (s *remoteSession) Resize(_ context.Context, size PtySize) error {
	if size.Rows <= 0 || size.Columns <= 0 {
		return fmt.Errorf("pty size must be greater than zero")
	}
	return s.session.WindowChange(size.Rows, size.Columns)
}

func (s *remoteSession) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		_ = s.stdin.Close()
		_ = s.writer.Close()
		if err := s.session.Close(); err != nil {
			closeErr = err
		}
		if err := s.client.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	})
	return closeErr
}

func (c *Client) hostKeyCallback() (ssh.HostKeyCallback, error) {
	if c.insecureIgnoreHostKey {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	return nil, fmt.Errorf("host key verification is not implemented for the current MVP; set %s=true", config.EnvSSHInsecureIgnoreHostKey())
}
