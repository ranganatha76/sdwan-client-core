package wireguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func testPlan(t *testing.T) Plan {
	t.Helper()
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	return Plan{Interface: "wg-pulseroute", Address: "10.90.0.2/32", ListenPort: 51820, Peers: []Peer{{PublicKey: key.PublicKey().String(), AllowedIPs: []string{"203.0.113.10/32"}, Endpoint: "192.0.2.1:51820", Keepalive: 25}}}
}

func TestProvisionPersistentPrivateIdentity(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "identity")
	if err := Initialize(directory); err != nil {
		t.Fatal(err)
	}
	private, _ := os.ReadFile(filepath.Join(directory, "private.key"))
	if err := Initialize(directory); err == nil {
		t.Fatal("reinitialization must fail")
	}
	plan := testPlan(t)
	if err := Configure(directory, plan); err != nil {
		t.Fatal(err)
	}
	if err := Configure(directory, plan); err == nil {
		t.Fatal("configuration overwrite must fail")
	}
	for _, name := range []string{"private.key", "public.key", plan.Interface + ".conf"} {
		info, err := os.Stat(filepath.Join(directory, name))
		if err != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatalf("unsafe file permissions for %s", name)
		}
	}
	config, _ := os.ReadFile(filepath.Join(directory, plan.Interface+".conf"))
	for _, expected := range []string{"Table = off", "PrivateKey = " + strings.TrimSpace(string(private)), "Endpoint = 192.0.2.1:51820", "PersistentKeepalive = 25"} {
		if !strings.Contains(string(config), expected) {
			t.Fatal("missing expected configuration directive")
		}
	}
}

func TestRejectUnsafePlans(t *testing.T) {
	cases := map[string]func(*Plan){
		"interface injection": func(plan *Plan) { plan.Interface = "wg\nPostUp=bad" },
		"default route":       func(plan *Plan) { plan.Peers[0].AllowedIPs = []string{"0.0.0.0/0"} },
		"IPv6":                func(plan *Plan) { plan.Address = "fd00::1/64" },
		"endpoint injection":  func(plan *Plan) { plan.Peers[0].Endpoint = "host\nPostUp=bad:123" },
		"bad key":             func(plan *Plan) { plan.Peers[0].PublicKey = "bad" },
		"duplicate peers":     func(plan *Plan) { plan.Peers = append(plan.Peers, plan.Peers[0]) },
		"overlap":             func(plan *Plan) { plan.Peers[0].AllowedIPs = []string{"203.0.113.0/24", "203.0.113.10/32"} },
		"local route":         func(plan *Plan) { plan.Peers[0].AllowedIPs = []string{"10.90.0.0/24"} },
		"noncanonical":        func(plan *Plan) { plan.Peers[0].AllowedIPs = []string{"203.0.113.10/24"} },
		"multicast":           func(plan *Plan) { plan.Peers[0].AllowedIPs = []string{"224.0.0.1/32"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			plan := testPlan(t)
			mutate(&plan)
			if err := plan.Validate(); err == nil {
				t.Fatal("unsafe plan accepted")
			}
		})
	}
}

func TestRejectLoosePermissions(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "identity")
	if err := Initialize(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(directory, "private.key"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Configure(directory, testPlan(t)); err == nil {
		t.Fatal("publicly readable private key accepted")
	}
}

func TestStrictPlanJSON(t *testing.T) {
	for _, body := range []string{`{"privateKey":"secret"}`, `{} {}`, `{}`} {
		if _, err := DecodePlan(strings.NewReader(body)); err == nil {
			t.Fatal("invalid or unknown plan accepted")
		}
	}
}
