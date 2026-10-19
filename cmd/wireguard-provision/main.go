package main

import (
	"flag"
	"fmt"
	"os"

	"sdwan-client-core/internal/wireguard"
)

func main() {
	directory := flag.String("identity-dir", "", "new local private identity directory")
	planPath := flag.String("plan", "", "public peer plan JSON file for configure")
	flag.Parse()
	if *directory == "" || flag.NArg() != 1 {
		fail(fmt.Errorf("usage: wireguard-provision --identity-dir PATH [--plan FILE] init|configure"))
	}
	switch flag.Arg(0) {
	case "init":
		if *planPath != "" {
			fail(fmt.Errorf("init does not accept a plan"))
		}
		if err := wireguard.Initialize(*directory); err != nil {
			fail(err)
		}
	case "configure":
		file, err := os.Open(*planPath)
		if err != nil {
			fail(fmt.Errorf("open public provisioning plan"))
		}
		defer file.Close()
		plan, err := wireguard.DecodePlan(file)
		if err != nil {
			fail(err)
		}
		if err := wireguard.Configure(*directory, plan); err != nil {
			fail(err)
		}
	default:
		fail(fmt.Errorf("command must be init or configure"))
	}
	fmt.Println("completed; private material remains in the identity directory")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "wireguard-provision:", err)
	os.Exit(1)
}
