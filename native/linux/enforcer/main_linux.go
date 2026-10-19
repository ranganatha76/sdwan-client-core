//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Config contains the root-owned routing parameters for the Linux adapter.
type Config struct {
	AgentURL        string `json:"agentURL"`
	WireGuardDevice string `json:"wireGuardDevice"`
	RouteTable      int    `json:"routeTable"`
	FirewallMark    int    `json:"firewallMark"`
}

type flowRequest struct {
	Hostname      string `json:"hostname"`
	DestinationIP string `json:"destinationIP"`
}

type decision struct {
	Action string `json:"action"`
}

var interfaceName = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,15}$`)

// main installs routing primitives or evaluates one classified destination.
func main() {
	configPath := flag.String("config", "/etc/pulseroute/enforcer.json", "root-owned enforcer configuration")
	hostname := flag.String("hostname", "", "hostname associated with the destination")
	destinationIP := flag.String("destination-ip", "", "destination IP address")
	flag.Parse()
	if flag.NArg() != 1 || (flag.Arg(0) != "install" && flag.Arg(0) != "evaluate") {
		fmt.Fprintln(os.Stderr, "usage: pulseroute-enforcer [--config PATH] [--hostname HOST --destination-ip IP] install|evaluate")
		os.Exit(2)
	}
	config, err := loadConfig(*configPath)
	if err != nil {
		fatal(err)
	}
	if flag.Arg(0) == "install" {
		if os.Geteuid() != 0 {
			fatal(errors.New("install requires root"))
		}
		fatal(install(context.Background(), config))
		return
	}

	if *hostname == "" || *destinationIP == "" {
		fatal(errors.New("evaluate requires --hostname and --destination-ip"))
	}
	fatal(evaluate(context.Background(), config, *hostname, *destinationIP))
}

// loadConfig reads and validates a root-owned configuration file without secrets.
func loadConfig(path string) (Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(contents, &config); err != nil {
		return Config{}, err
	}
	if config.AgentURL == "" || !interfaceName.MatchString(config.WireGuardDevice) || config.RouteTable < 1 || config.FirewallMark < 1 {
		return Config{}, errors.New("config requires agentURL, safe wireGuardDevice, positive routeTable, and positive firewallMark")
	}
	return config, nil
}

// install creates a dedicated mark, nftables destination set, and WireGuard route table.
func install(ctx context.Context, config Config) error {
	commands := [][]string{
		{"ip", "route", "replace", "default", "dev", config.WireGuardDevice, "table", fmt.Sprint(config.RouteTable)},
		{"ip", "rule", "replace", "fwmark", fmt.Sprint(config.FirewallMark), "lookup", fmt.Sprint(config.RouteTable)},
		{"nft", "add", "table", "inet", "pulseroute"},
		{"nft", "add", "set", "inet", "pulseroute", "priority_destinations", "{", "type", "ipv4_addr", ";", "flags", "timeout", ";", "}"},
		{"nft", "add", "chain", "inet", "pulseroute", "output", "{", "type", "route", "hook", "output", "priority", "mangle", ";", "policy", "accept", ";", "ip", "daddr", "@priority_destinations", "meta", "mark", "set", fmt.Sprint(config.FirewallMark), ";", "}"},
	}
	for _, command := range commands {
		if err := run(ctx, command[0], command[1:]...); err != nil {
			return fmt.Errorf("install %s: %w", strings.Join(command, " "), err)
		}
	}
	return nil
}

// evaluate asks the decision agent whether a destination should receive the priority mark.
func evaluate(ctx context.Context, config Config, hostname, destinationIP string) error {
	body, err := json.Marshal(flowRequest{Hostname: hostname, DestinationIP: destinationIP})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(config.AgentURL, "/")+"/v1/flows/evaluate", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("decision agent returned %s", response.Status)
	}
	var result decision
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&result); err != nil {
		return err
	}
	if result.Action != "priority-overlay" {
		return nil
	}
	if os.Geteuid() != 0 {
		return errors.New("priority decision received, but adding an nftables destination requires root")
	}
	return run(ctx, "nft", "add", "element", "inet", "pulseroute", "priority_destinations", "{", destinationIP, "timeout", "10m", "}")
}

// run invokes one fixed system command without a shell.
func run(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

// fatal exits only when a command reports an error.
func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "pulseroute-enforcer:", err)
		os.Exit(1)
	}
}