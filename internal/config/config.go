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
	envConfigFile               = "GATEWAY_CONFIG_FILE"
	envListenAddress            = "GATEWAY_LISTEN_ADDRESS"
	envBackendBaseURL           = "GATEWAY_BACKEND_BASE_URL"
	envBackendTimeout           = "GATEWAY_BACKEND_TIMEOUT"
	envGrantHeaderName          = "GATEWAY_GRANT_HEADER_NAME"
	envLogLevel                 = "GATEWAY_LOG_LEVEL"
	envSSHPrivateKeyPath        = "GATEWAY_SSH_PRIVATE_KEY_PATH"
	envSSHPublicKeyPath         = "GATEWAY_SSH_PUBLIC_KEY_PATH"
	envSSHUsername              = "GATEWAY_SSH_USERNAME"
	envSSHPort                  = "GATEWAY_SSH_PORT"
	envSSHConnectTimeout        = "GATEWAY_SSH_CONNECT_TIMEOUT"
	envSSHInsecureIgnoreHostKey = "GATEWAY_SSH_INSECURE_IGNORE_HOST_KEY"
)

const (
	defaultBackendTimeout           = 5 * time.Second
	defaultGrantHeaderName          = "X-Rook-Terminal-Grant"
	defaultLogLevel                 = slog.LevelInfo
	defaultSSHPrivateKey            = "secrets/gateway_ssh_ed25519"
	defaultSSHPublicKey             = "secrets/gateway_ssh_ed25519.pub"
	defaultSSHUsername              = "pi"
	defaultSSHPort                  = 22
	defaultSSHConnectTimeout        = 5 * time.Second
	defaultSSHInsecureIgnoreHostKey = true
)

type Config struct {
	HTTP    HTTPConfig
	Backend BackendConfig
	Logging LoggingConfig
	Secrets SecretsConfig
	SSH     SSHConfig
}

type HTTPConfig struct {
	ListenAddress   string
	GrantHeaderName string
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
			ListenAddress:   strings.TrimSpace(vars[envListenAddress]),
			GrantHeaderName: firstNonEmpty(vars[envGrantHeaderName], defaultGrantHeaderName),
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
	}

	timeoutValue := firstNonEmpty(vars[envBackendTimeout], defaultBackendTimeout.String())
	timeout, err := time.ParseDuration(timeoutValue)
	if err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", envBackendTimeout, err)
	}
	cfg.Backend.ValidationTimeout = timeout

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
	if strings.TrimSpace(c.HTTP.GrantHeaderName) == "" {
		return fmt.Errorf("%s must not be empty", envGrantHeaderName)
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
		envBackendBaseURL,
		envBackendTimeout,
		envGrantHeaderName,
		envLogLevel,
		envSSHPrivateKeyPath,
		envSSHPublicKeyPath,
		envSSHUsername,
		envSSHPort,
		envSSHConnectTimeout,
		envSSHInsecureIgnoreHostKey,
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
