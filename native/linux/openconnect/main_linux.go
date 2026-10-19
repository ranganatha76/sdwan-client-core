//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

const defaultConfigPath = "/etc/pulseroute/openconnect.json"

var interfaceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,15}$`)

// Config contains only non-secret OpenConnect connection settings owned by the administrator.
// Authentication remains interactive in the OpenConnect process or uses an organization-managed token provider.
type Config struct {
	Protocol          string `json:"protocol"`
	Gateway           string `json:"gateway"`
	Interface         string `json:"interface"`
	AuthGroup         string `json:"authGroup,omitempty"`
	Username          string `json:"username,omitempty"`
	ServerCertificate string `json:"serverCertificate,omitempty"`
	PIDFile           string `json:"pidFile"`
}

type status struct {
	Connected bool   `json:"connected"`
	Interface string `json:"interface"`
	PID       int    `json:"pid,omitempty"`
}

// main manages an OpenConnect-compatible tunnel without handling credentials itself.
func main() {
	configPath := flag.String("config", defaultConfigPath, "root-owned OpenConnect configuration")
	flag.Parse()
	if flag.NArg() != 1 {
		fatal(errors.New("usage: pulseroute-openconnect [--config PATH] connect|disconnect|status"))
	}
	config, err := loadConfig(*configPath)
	if err != nil {
		fatal(err)
	}

	switch flag.Arg(0) {
	case "connect":
		fatal(connect(config))
	case "disconnect":
		fatal(disconnect(config))
	case "status":
		fatal(writeStatus(config))
	default:
		fatal(errors.New("usage: pulseroute-openconnect [--config PATH] connect|disconnect|status"))
	}
}

// loadConfig reads and verifies an administrator-owned, non-secret configuration file.
func loadConfig(path string) (Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Config{}, err
	}
	if err := validateConfigFile(info); err != nil {
		return Config{}, err
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(contents, &config); err != nil {
		return Config{}, err
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func validateConfigFile(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || info.Mode().Perm()&0o077 != 0 {
		return errors.New("OpenConnect configuration must be root-owned and mode 0600 or stricter")
	}
	return nil
}

// Validate prevents configuration from adding unsupported protocols or unsafe process arguments.
func (c Config) Validate() error {
	if c.Protocol != "anyconnect" && c.Protocol != "gp" {
		return errors.New("protocol must be anyconnect or gp")
	}
	gateway, err := url.ParseRequestURI(c.Gateway)
	if err != nil || gateway.Scheme != "https" || gateway.Host == "" || gateway.User != nil || gateway.Fragment != "" {
		return errors.New("gateway must be an HTTPS URL without embedded credentials or a fragment")
	}
	if !interfaceNamePattern.MatchString(c.Interface) || !safeOption(c.AuthGroup) || !safeOption(c.Username) || !safeOption(c.ServerCertificate) {
		return errors.New("interface, auth group, username, and server certificate contain unsupported characters")
	}
	pidFile := filepath.Clean(c.PIDFile)
	if !filepath.IsAbs(pidFile) || !strings.HasPrefix(pidFile, "/run/pulseroute/") {
		return errors.New("pidFile must be under /run/pulseroute")
	}
	return nil
}

func safeOption(value string) bool {
	return !strings.ContainsAny(value, "\x00\n\r")
}

// openConnectArguments returns a fixed allowlist of non-secret OpenConnect settings.
func openConnectArguments(config Config) []string {
	arguments := []string{
		"--protocol=" + config.Protocol,
		"--interface=" + config.Interface,
		"--background",
		"--pid-file=" + config.PIDFile,
		"--timestamp",
		"--reconnect-timeout=60",
	}
	if config.AuthGroup != "" {
		arguments = append(arguments, "--authgroup="+config.AuthGroup)
	}
	if config.Username != "" {
		arguments = append(arguments, "--user="+config.Username)
	}
	if config.ServerCertificate != "" {
		arguments = append(arguments, "--servercert="+config.ServerCertificate)
	}
	return append(arguments, config.Gateway)
}

// connect starts the system OpenConnect client and leaves authentication on the administrator's terminal.
func connect(config Config) error {
	if os.Geteuid() != 0 {
		return errors.New("connect requires root")
	}
	if existing := readPID(config.PIDFile); existing > 0 && processAlive(existing) {
		return fmt.Errorf("OpenConnect tunnel is already running with PID %d", existing)
	}
	if _, err := exec.LookPath("openconnect"); err != nil {
		return errors.New("openconnect is not installed")
	}
	if err := os.MkdirAll(filepath.Dir(config.PIDFile), 0o750); err != nil {
		return err
	}
	command := exec.Command("openconnect", openConnectArguments(config)...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

// disconnect sends the clean OpenConnect shutdown signal to the PID recorded for this managed tunnel.
func disconnect(config Config) error {
	if os.Geteuid() != 0 {
		return errors.New("disconnect requires root")
	}
	pid := readPID(config.PIDFile)
	if pid <= 0 || !processAlive(pid) {
		return errors.New("managed OpenConnect tunnel is not running")
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

func writeStatus(config Config) error {
	pid := readPID(config.PIDFile)
	_, interfaceErr := net.InterfaceByName(config.Interface)
	current := status{Connected: pid > 0 && processAlive(pid) && interfaceErr == nil, Interface: config.Interface}
	if current.Connected {
		current.PID = pid
	}
	return json.NewEncoder(os.Stdout).Encode(current)
}

func readPID(path string) int {
	contents, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil || errors.Is(syscall.Kill(pid, 0), syscall.EPERM)
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "pulseroute-openconnect:", err)
		os.Exit(1)
	}
}
