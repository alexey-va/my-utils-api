package wireguardops

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRoutingReconcilerUsesPublicHairpinServiceEnvironment(t *testing.T) {
	for _, test := range []struct {
		name   string
		active bool
	}{
		{name: "active_with_config_files", active: true},
		{name: "inactive_without_config_files", active: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			routingEnv := tempDir + "/routing-ha.env"
			hairpinEnv := tempDir + "/public-hairpin.env"
			callsFile := tempDir + "/hairpin.calls"
			if test.active {
				if err := os.WriteFile(routingEnv, []byte("WIREGUARD_CLIENT_CIDR=10.89.0.0/24\nWIREGUARD_INGRESS_INTERFACE=wg-users\nWIREGUARD_ROUTE_TABLE=51889\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(hairpinEnv, []byte("WIREGUARD_PUBLIC_ADDRESS=203.0.113.7\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			writeExecutable(t, tempDir+"/systemctl", `#!/bin/sh
case "$*" in
  'is-active --quiet my-utils-wireguard-routing.service') exit 0 ;;
  'is-active --quiet my-utils-wireguard-public-hairpin.service') test "$HAIRPIN_ACTIVE" = true ;;
  'is-active --quiet '*) exit 3 ;;
  *) printf 'Unexpected systemctl command: %s\n' "$*" >&2; exit 2 ;;
esac
`)
			writeExecutable(t, tempDir+"/ip", `#!/bin/sh
case "$*" in
  '-4 rule show')
    printf '%s\n' '0: from all lookup local' '1089: from 10.89.0.0/24 lookup 51889'
    ;;
  '-4 route show table 51889')
    printf '%s\n' '10.89.0.0/24 dev wg-users scope link' 'unreachable default metric 32767'
    ;;
  *) printf 'Unexpected ip command: %s\n' "$*" >&2; exit 2 ;;
esac
`)
			writeExecutable(t, tempDir+"/flock", `#!/bin/sh
echo 'Healthy routing must not acquire repair locks' >&2
exit 2
`)
			writeExecutable(t, tempDir+"/my-utils-wireguard-public-hairpin", `#!/bin/sh
set -eu
test "$#" -eq 1
test "$1" = start
test "$WIREGUARD_PUBLIC_ADDRESS" = 203.0.113.7
test "$WIREGUARD_CLIENT_CIDR" = 10.89.0.0/24
test "$WIREGUARD_INGRESS_INTERFACE" = wg-users
test "$WIREGUARD_ROUTE_TABLE" = 51889
printf '%s\n' "$1" >>"$HAIRPIN_CALLS_FILE"
`)

			command := exec.Command("bash", "routing-reconcile.sh")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "WIREGUARD_") {
					command.Env = append(command.Env, entry)
				}
			}
			active := "false"
			if test.active {
				active = "true"
			}
			command.Env = append(command.Env,
				"PATH="+tempDir+":"+os.Getenv("PATH"),
				"HAIRPIN_ACTIVE="+active,
				"HAIRPIN_CALLS_FILE="+callsFile,
				"WIREGUARD_ROUTING_LIBEXEC_DIR="+tempDir,
				"WIREGUARD_ROUTING_ENV_FILE="+routingEnv,
				"WIREGUARD_PUBLIC_HAIRPIN_ENV_FILE="+hairpinEnv,
				"WIREGUARD_ROUTING_RECONCILE_LOCK_FILE="+tempDir+"/reconcile.lock",
				"WIREGUARD_FAILOVER_LOCK_FILE="+tempDir+"/failover.lock",
			)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("healthy reconcile failed: %v\n%s", err, output)
			}
			if test.active {
				if calls := readFile(t, callsFile); calls != "start\n" {
					t.Fatalf("hairpin calls = %q, want exactly one start", calls)
				}
			} else if _, err := os.Stat(callsFile); !os.IsNotExist(err) {
				t.Fatalf("inactive hairpin helper was called: stat error = %v", err)
			}
		})
	}
}
