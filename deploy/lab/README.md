# PulseRoute Linux Lab

This zero-cost Linux-VM lab proves that a managed laptop can reach an approved service through either of two WireGuard gateway paths and that an impaired primary path is measurably worse than the healthy backup path.

It creates three temporary network namespaces on one Linux VM:

```text
pulseroute-client -- primary/backup WireGuard tunnels -- pulseroute-gateway -- pulseroute-saas
```

The script creates short-lived WireGuard keys in memory, does not write keys to the repository, and deletes all lab namespaces on teardown. It is for local validation only; it does not install the PulseRoute Linux enforcer or modify the host's normal routes.

## Prerequisites

- Linux VM with kernel WireGuard support
- `iproute2`, `wireguard-tools`, `iputils-ping`, and `bash`
- Root access inside the VM

Run this on a Linux VM, not Docker Desktop or macOS. The lab requires Linux network namespaces, `tc netem`, and the WireGuard kernel module.

## Run

```sh
cd deploy/lab
chmod 700 pulseroute-lab.sh
sudo ./pulseroute-lab.sh up
sudo ./pulseroute-lab.sh measure
```

The initial measurement uses the primary tunnel. Impair that underlay, measure again, then select the healthy backup tunnel:

```sh
sudo ./pulseroute-lab.sh impair-primary
sudo ./pulseroute-lab.sh measure
sudo ./pulseroute-lab.sh prefer backup
sudo ./pulseroute-lab.sh measure
```

The three measurements are the evidence for a pilot demo: baseline primary, impaired primary, and recovered backup. Capture packet loss and round-trip latency from the command output; do not call an avoided incident until an approved application SLO producer submits actual before/after evidence.

Restore or remove the lab at any time:

```sh
sudo ./pulseroute-lab.sh restore-primary
sudo ./pulseroute-lab.sh down
```

## Connect To PulseRoute

The lab is intentionally isolated from the host control plane. To connect it to the Linux enforcer, install the enforcer inside a client Linux VM, configure it with the selected WireGuard device, and invoke it only after a trusted hostname-to-IP collector calls the local data-plane agent. The lab provides the two gateway paths and controlled impairment needed to test that integration without any paid VPN or SSE vendor dependency.