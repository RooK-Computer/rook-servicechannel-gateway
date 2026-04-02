package config

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	envConfigFile                 = "GATEWAY_CONFIG_FILE"
	envListenAddress              = "GATEWAY_LISTEN_ADDRESS"
	envHTTPReadHeaderTimeout      = "GATEWAY_HTTP_READ_HEADER_TIMEOUT"
	envBackendBaseURL             = "GATEWAY_BACKEND_BASE_URL"
	envBackendTimeout             = "GATEWAY_BACKEND_TIMEOUT"
	envLogLevel                   = "GATEWAY_LOG_LEVEL"
	envSSHPrivateKeyPath          = "GATEWAY_SSH_PRIVATE_KEY_PATH"
	envSSHPublicKeyPath           = "GATEWAY_SSH_PUBLIC_KEY_PATH"
	envSSHUsername                = "GATEWAY_SSH_USERNAME"
	envSSHPort                    = "GATEWAY_SSH_PORT"
	envSSHConnectTimeout          = "GATEWAY_SSH_CONNECT_TIMEOUT"
	envSSHInsecureIgnoreHostKey   = "GATEWAY_SSH_INSECURE_IGNORE_HOST_KEY"
	envSessionAuthorizeTimeout    = "GATEWAY_SESSION_AUTHORIZE_TIMEOUT"
	envSessionIdleTimeout         = "GATEWAY_SESSION_IDLE_TIMEOUT"
	envSessionMaxConcurrent       = "GATEWAY_SESSION_MAX_CONCURRENT"
	envSessionOutboundQueue       = "GATEWAY_SESSION_OUTBOUND_QUEUE_DEPTH"
	envWebSocketMaxMessageBytes   = "GATEWAY_WEBSOCKET_MAX_MESSAGE_BYTES"
	envWebSocketKeepaliveInterval = "GATEWAY_WEBSOCKET_KEEPALIVE_INTERVAL"
	envWebSocketKeepaliveTimeout  = "GATEWAY_WEBSOCKET_KEEPALIVE_TIMEOUT"
)

const (
	defaultHTTPReadHeaderTimeout      = 5 * time.Second
	defaultBackendTimeout             = 5 * time.Second
	defaultLogLevel                   = slog.LevelInfo
	defaultSSHPrivateKey              = "secrets/gateway_ssh_ed25519"
	defaultSSHPublicKey               = "secrets/gateway_ssh_ed25519.pub"
	defaultSSHUsername                = "pi"
	defaultSSHPort                    = 22
	defaultSSHConnectTimeout          = 5 * time.Second
	defaultSSHInsecureIgnoreHostKey   = true
	defaultSessionAuthorizeTimeout    = 2 * time.Minute
	defaultSessionMaxConcurrent       = 32
	defaultSessionOutboundQueue       = 16
	defaultWebSocketMaxMessageBytes   = int64(64 * 1024)
	defaultWebSocketKeepaliveInterval = 30 * time.Second
	defaultWebSocketKeepaliveTimeout  = 75 * time.Second
)

type Config struct {
	HTTP      HTTPConfig
	Backend   BackendConfig
	Logging   LoggingConfig
	Secrets   SecretsConfig
	SSH       SSHConfig
	Session   SessionConfig
	WebSocket WebSocketConfig
}

type HTTPConfig struct {
	ListenAddress     string
	ReadHeaderTimeout time.Duration
}

type BackendConfig struct {
	BaseURL           string
	ValidationTimeout time.Duration
}

type LoggingConfig struct {
	Level slog.Level
}

type SecretsConfig struct {
	SSHPrivateKeyPath string
	SSHPublicKeyPath  string
}

type SSHConfig struct {
	Username              string
	Port                  int
	ConnectTimeout        time.Duration
	InsecureIgnoreHostKey bool
}

type SessionConfig struct {
	AuthorizeTimeout   time.Duration
	MaxConcurrent      int
	OutboundQueueDepth int
}

type WebSocketConfig struct {
	MaxMessageBytes   int64
	KeepaliveInterval time.Duration
	KeepaliveTimeout  time.Duration
}

func Load() (Config, error) {
	vars, err := loadVars(os.LookupEnv)
	if err != nil {
		return Config{}, err
	}

	return Resolve(vars)
}

func Resolve(vars map[string]string) (Config, error) {
	cfg := Config{
		HTTP: HTTPConfig{
			ListenAddress:     strings.TrimSpace(vars[envListenAddress]),
			ReadHeaderTimeout: defaultHTTPReadHeaderTimeout,
		},
		Backend: BackendConfig{
			BaseURL: strings.TrimSpace(vars[envBackendBaseURL]),
		},
		Logging: LoggingConfig{Level: defaultLogLevel},
		Secrets: SecretsConfig{
			SSHPrivateKeyPath: firstNonEmpty(vars[envSSHPrivateKeyPath], defaultSSHPrivateKey),
			SSHPublicKeyPath:  firstNonEmpty(vars[envSSHPublicKeyPath], defaultSSHPublicKey),
		},
		SSH: SSHConfig{
			Username:              firstNonEmpty(vars[envSSHUsername], defaultSSHUsername),
			Port:                  defaultSSHPort,
			ConnectTimeout:        defaultSSHConnectTimeout,
			InsecureIgnoreHostKey: defaultSSHInsecureIgnoreHostKey,
		},
		Session: SessionConfig{
			AuthorizeTimeout:   defaultSessionAuthorizeTimeout,
			MaxConcurrent:      defaultSessionMaxConcurrent,
			OutboundQueueDepth: defaultSessionOutboundQueue,
		},
		WebSocket: WebSocketConfig{
			MaxMessageBytes:   defaultWebSocketMaxMessageBytes,
			KeepaliveInterval: defaultWebSocketKeepaliveInterval,
			KeepaliveTimeout:  defaultWebSocketKeepaliveTimeout,
		},
	}

	timeoutValue := firstNonEmpty(vars[envBackendTimeout], defaultBackendTimeout.String())
	timeout, err := time.ParseDuration(timeoutValue)
	if err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", envBackendTimeout, err)
	}
	cfg.Backend.ValidationTimeout = timeout

	if readHeaderTimeoutValue := strings.TrimSpace(vars[envHTTPReadHeaderTimeout]); readHeaderTimeoutValue != "" {
		parsedTimeout, err := time.ParseDuration(readHeaderTimeoutValue)
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", envHTTPReadHeaderTimeout, err)
		}
		cfg.HTTP.ReadHeaderTimeout = parsedTimeout
	}

	if portValue := strings.TrimSpace(vars[envSSHPort]); portValue != "" {
		port, err := strconv.Atoi(portValue)
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", envSSHPort, err)
		}
		cfg.SSH.Port = port
	}

	if connectTimeoutValue := strings.TrimSpace(vars[envSSHConnectTimeout]); connectTimeoutValue != "" {
		parsedTimeout, err := time.ParseDuration(connectTimeoutValue)
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", envSSHConnectTimeout, err)
		}
		cfg.SSH.ConnectTimeout = parsedTimeout
	}

	if insecureValue := strings.TrimSpace(vars[envSSHInsecureIgnoreHostKey]); insecureValue != "" {
		parsedBool, err := strconv.ParseBool(insecureValue)
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", envSSHInsecureIgnoreHostKey, err)
		}
		cfg.SSH.InsecureIgnoreHostKey = parsedBool
	}

	authorizeTimeoutValue := strings.TrimSpace(vars[envSessionAuthorizeTimeout])
	if authorizeTimeoutValue == "" {
		authorizeTimeoutValue = strings.TrimSpace(vars[envSessionIdleTimeout])
	}
	if authorizeTimeoutValue != "" {
		parsedTimeout, err := time.ParseDuration(authorizeTimeoutValue)
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", envSessionAuthorizeTimeout, err)
		}
		cfg.Session.AuthorizeTimeout = parsedTimeout
	}

	if maxConcurrentValue := strings.TrimSpace(vars[envSessionMaxConcurrent]); maxConcurrentValue != "" {
		parsedValue, err := strconv.Atoi(maxConcurrentValue)
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", envSessionMaxConcurrent, err)
		}
		cfg.Session.MaxConcurrent = parsedValue
	}

	if outboundQueueValue := strings.TrimSpace(vars[envSessionOutboundQueue]); outboundQueueValue != "" {
		parsedValue, err := strconv.Atoi(outboundQueueValue)
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", envSessionOutboundQueue, err)
		}
		cfg.Session.OutboundQueueDepth = parsedValue
	}

	if maxMessageBytesValue := strings.TrimSpace(vars[envWebSocketMaxMessageBytes]); maxMessageBytesValue != "" {
		parsedValue, err := strconv.ParseInt(maxMessageBytesValue, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", envWebSocketMaxMessageBytes, err)
		}
		cfg.WebSocket.MaxMessageBytes = parsedValue
	}
	if keepaliveIntervalValue := strings.TrimSpace(vars[envWebSocketKeepaliveInterval]); keepaliveIntervalValue != "" {
		parsedValue, err := time.ParseDuration(keepaliveIntervalValue)
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", envWebSocketKeepaliveInterval, err)
		}
		cfg.WebSocket.KeepaliveInterval = parsedValue
	}
	if keepaliveTimeoutValue := strings.TrimSpace(vars[envWebSocketKeepaliveTimeout]); keepaliveTimeoutValue != "" {
		parsedValue, err := time.ParseDuration(keepaliveTimeoutValue)
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", envWebSocketKeepaliveTimeout, err)
		}
		cfg.WebSocket.KeepaliveTimeout = parsedValue
	}

	if levelValue := strings.TrimSpace(vars[envLogLevel]); levelValue != "" {
		level, err := parseLogLevel(levelValue)
		if err != nil {
			return Config{}, err
		}
		cfg.Logging.Level = level
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.HTTP.ListenAddress) == "" {
		return fmt.Errorf("%s must be set", envListenAddress)
	}
	if strings.TrimSpace(c.Backend.BaseURL) == "" {
		return fmt.Errorf("%s must be set", envBackendBaseURL)
	}

	parsedURL, err := url.Parse(c.Backend.BaseURL)
	if err != nil {
		return fmt.Errorf("parse %s: %w", envBackendBaseURL, err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", envBackendBaseURL)
	}
	if parsedURL.Host == "" {
		return fmt.Errorf("%s must include a host", envBackendBaseURL)
	}
	if c.Backend.ValidationTimeout <= 0 {
		return fmt.Errorf("%s must be greater than zero", envBackendTimeout)
	}
	if c.HTTP.ReadHeaderTimeout <= 0 {
		return fmt.Errorf("%s must be greater than zero", envHTTPReadHeaderTimeout)
	}
	if strings.TrimSpace(c.Secrets.SSHPrivateKeyPath) == "" {
		return fmt.Errorf("%s must not be empty", envSSHPrivateKeyPath)
	}
	if strings.TrimSpace(c.Secrets.SSHPublicKeyPath) == "" {
		return fmt.Errorf("%s must not be empty", envSSHPublicKeyPath)
	}
	if strings.TrimSpace(c.SSH.Username) == "" {
		return fmt.Errorf("%s must not be empty", envSSHUsername)
	}
	if c.SSH.Port <= 0 || c.SSH.Port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", envSSHPort)
	}
	if c.SSH.ConnectTimeout <= 0 {
		return fmt.Errorf("%s must be greater than zero", envSSHConnectTimeout)
	}
	if c.Session.AuthorizeTimeout <= 0 {
		return fmt.Errorf("%s must be greater than zero", envSessionAuthorizeTimeout)
	}
	if c.Session.MaxConcurrent <= 0 {
		return fmt.Errorf("%s must be greater than zero", envSessionMaxConcurrent)
	}
	if c.Session.OutboundQueueDepth <= 0 {
		return fmt.Errorf("%s must be greater than zero", envSessionOutboundQueue)
	}
	if c.WebSocket.MaxMessageBytes <= 0 {
		return fmt.Errorf("%s must be greater than zero", envWebSocketMaxMessageBytes)
	}
	if c.WebSocket.KeepaliveInterval <= 0 {
		return fmt.Errorf("%s must be greater than zero", envWebSocketKeepaliveInterval)
	}
	if c.WebSocket.KeepaliveTimeout <= 0 {
		return fmt.Errorf("%s must be greater than zero", envWebSocketKeepaliveTimeout)
	}
	if c.WebSocket.KeepaliveTimeout <= c.WebSocket.KeepaliveInterval {
		return fmt.Errorf("%s must be greater than %s", envWebSocketKeepaliveTimeout, envWebSocketKeepaliveInterval)
	}
	return nil
}

func parseLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid %s %q", envLogLevel, value)
	}
}

func loadVars(lookup func(string) (string, bool)) (map[string]string, error) {
	vars := map[string]string{}

	configFile, ok := lookup(envConfigFile)
	if ok && strings.TrimSpace(configFile) != "" {
		fileVars, err := loadFile(configFile)
		if err != nil {
			return nil, err
		}
		for key, value := range fileVars {
			vars[key] = value
		}
	}

	for _, key := range []string{
		envConfigFile,
		envListenAddress,
		envHTTPReadHeaderTimeout,
		envBackendBaseURL,
		envBackendTimeout,
		envLogLevel,
		envSSHPrivateKeyPath,
		envSSHPublicKeyPath,
		envSSHUsername,
		envSSHPort,
		envSSHConnectTimeout,
		envSSHInsecureIgnoreHostKey,
		envSessionAuthorizeTimeout,
		envSessionIdleTimeout,
		envSessionMaxConcurrent,
		envSessionOutboundQueue,
		envWebSocketMaxMessageBytes,
		envWebSocketKeepaliveInterval,
		envWebSocketKeepaliveTimeout,
	} {
		if value, ok := lookup(key); ok {
			vars[key] = value
		}
	}

	return vars, nil
}

func loadFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", envConfigFile, err)
	}
	defer file.Close()

	vars := map[string]string{}
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("parse %s line %d: expected KEY=VALUE", path, lineNumber)
		}

		vars[strings.TrimSpace(key)] = trimQuotes(strings.TrimSpace(value))
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return vars, nil
}

func trimQuotes(value string) string {
	if len(value) < 2 {
		return value
	}
	if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "\""), "\"")
	}
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "'"), "'")
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

var ErrMissingConfig = errors.New("missing configuration")

func EnvSSHInsecureIgnoreHostKey() string {
	return envSSHInsecureIgnoreHostKey
}
