package wireguard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type Plan struct {
	Interface  string `json:"interface"`
	Address    string `json:"address"`
	ListenPort int    `json:"listenPort"`
	Peers      []Peer `json:"peers"`
}

type Peer struct {
	PublicKey  string   `json:"publicKey"`
	AllowedIPs []string `json:"allowedIPs"`
	Endpoint   string   `json:"endpoint,omitempty"`
	Keepalive  int      `json:"keepalive,omitempty"`
}

var interfaceName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,14}$`)
var endpointName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,252}$`)

func DecodePlan(reader io.Reader) (Plan, error) {
	var plan Plan
	decoder := json.NewDecoder(io.LimitReader(reader, (64<<10)+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return Plan{}, errors.New("invalid provisioning plan JSON")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return Plan{}, errors.New("plan must contain exactly one JSON document")
	}
	return plan, plan.Validate()
}

func (plan Plan) Validate() error {
	address, err := netip.ParsePrefix(plan.Address)
	if !interfaceName.MatchString(plan.Interface) || err != nil || !address.Addr().Is4() ||
		!address.Addr().IsGlobalUnicast() || address.Bits() == 0 || plan.ListenPort < 1 || plan.ListenPort > 65535 || len(plan.Peers) > 256 {
		return errors.New("require safe interface, unicast IPv4 address, valid listen port, and at most 256 peers")
	}
	keys := make(map[string]bool)
	var prefixes []netip.Prefix
	for _, peer := range plan.Peers {
		key, err := wgtypes.ParseKey(peer.PublicKey)
		if err != nil || key == (wgtypes.Key{}) || keys[peer.PublicKey] {
			return errors.New("peer public keys must be valid, nonzero, and unique")
		}
		keys[peer.PublicKey] = true
		if len(peer.AllowedIPs) == 0 || len(peer.AllowedIPs) > 64 || peer.Keepalive < 0 || peer.Keepalive > 65535 {
			return errors.New("peer requires bounded allowed IPs and valid keepalive")
		}
		if peer.Endpoint != "" {
			host, port, err := net.SplitHostPort(peer.Endpoint)
			portNumber, portErr := strconv.Atoi(port)
			if err != nil || portErr != nil || portNumber < 1 || portNumber > 65535 || !endpointName.MatchString(host) {
				return errors.New("endpoint must be an IPv4 address or hostname with a valid port")
			}
		}
		for _, value := range peer.AllowedIPs {
			prefix, err := netip.ParsePrefix(value)
			if err != nil || !prefix.Addr().Is4() || !prefix.Addr().IsGlobalUnicast() || prefix.Bits() == 0 || prefix != prefix.Masked() || prefix.Contains(address.Addr()) {
				return errors.New("allowed IPs must be canonical IPv4 unicast prefixes excluding default and local address")
			}
			for _, existing := range prefixes {
				if prefix.Overlaps(existing) {
					return errors.New("peer allowed IP ranges must not overlap")
				}
			}
			prefixes = append(prefixes, prefix)
		}
	}
	return nil
}

func Initialize(directory string) error {
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return errors.New("generate WireGuard private key")
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return errors.New("identity directory must not already exist and its parent must exist")
	}
	if err := writeNew(filepath.Join(directory, "private.key"), []byte(key.String()+"\n")); err != nil {
		return err
	}
	return writeNew(filepath.Join(directory, "public.key"), []byte(key.PublicKey().String()+"\n"))
}

func Configure(directory string, plan Plan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("identity directory must be a private real directory")
	}
	keyPath := filepath.Join(directory, "private.key")
	info, err = os.Lstat(keyPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 128 {
		return errors.New("private key must be a private regular file")
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return errors.New("read private key")
	}
	key, err := wgtypes.ParseKey(strings.TrimSpace(string(data)))
	if err != nil || key == (wgtypes.Key{}) {
		return errors.New("invalid private key")
	}
	var config strings.Builder
	fmt.Fprintf(&config, "[Interface]\nPrivateKey = %s\nAddress = %s\nListenPort = %d\nTable = off\n", key, plan.Address, plan.ListenPort)
	for _, peer := range plan.Peers {
		if peer.PublicKey == key.PublicKey().String() {
			return errors.New("a device cannot peer with itself")
		}
		fmt.Fprintf(&config, "\n[Peer]\nPublicKey = %s\nAllowedIPs = %s\n", peer.PublicKey, strings.Join(peer.AllowedIPs, ", "))
		if peer.Endpoint != "" {
			fmt.Fprintf(&config, "Endpoint = %s\n", peer.Endpoint)
		}
		if peer.Keepalive != 0 {
			fmt.Fprintf(&config, "PersistentKeepalive = %d\n", peer.Keepalive)
		}
	}
	return writeNew(filepath.Join(directory, plan.Interface+".conf"), []byte(config.String()))
}

func writeNew(destination string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(destination), ".provision-*")
	if err != nil {
		return errors.New("create private configuration staging file")
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return errors.New("write private configuration")
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return errors.New("sync private configuration")
	}
	if err := file.Close(); err != nil {
		return errors.New("close private configuration")
	}
	if err := os.Link(file.Name(), destination); err != nil {
		return errors.New("publish configuration: destination must not already exist")
	}
	return nil
}
