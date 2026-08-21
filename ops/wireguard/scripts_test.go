package wireguardops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func TestDesiredStateValidatorAcceptsOnlyPrivateIPv4Hosts(t *testing.T) {
	script := readFile(t, "wireguard-agent.sh")
	const marker = `(.allowedIp | type == "string" and test("`
	_, tail, found := strings.Cut(script, marker)
	if !found {
		t.Fatalf("validator marker %q not found", marker)
	}
	patternText, _, found := strings.Cut(tail, `"))`)
	if !found {
		t.Fatal("validator pattern terminator not found")
	}
	pattern := regexp.MustCompile(strings.ReplaceAll(patternText, `\\`, `\`))

	for _, value := range []string{"10.89.0.2/32", "172.16.0.2/32", "192.168.10.2/32"} {
		if !pattern.MatchString(value) {
			t.Errorf("validator rejected %q", value)
		}
	}
	for _, value := range []string{"8.8.8.8/32", "10.89.0.0/24"} {
		if pattern.MatchString(value) {
			t.Errorf("validator accepted %q", value)
		}
	}
}

func TestHeartbeatKeepsRoutingStatusAndCountersNonSecret(t *testing.T) {
	script := readFile(t, "wireguard-agent.sh") + readFile(t, "route-probe.sh")
	for _, want := range []string{
		"WIREGUARD_ROUTING_STATUS_FILE",
		"WIREGUARD_EXIT_HEALTH_FILE",
		"WIREGUARD_EXIT_PREFERENCE_FILE",
		`(.exitPreference == "AUTO" or .exitPreference == "PRIMARY" or .exitPreference == "SECONDARY")`,
		`jq -r '.exitPreference' "$desired_json"`,
		"systemctl start my-utils-awg-failover.service",
		"routingStatus: $routingStatus[0]",
		"exitHealth: $exitHealth[0]",
		`mode == "RU_DIRECT_AWG_DEFAULT"`,
		"my-utils-awg-failover.timer",
		"my-utils-geo-routing.service",
		"my-utils-wireguard-dns.service",
		"MYUTILS-WG-DNS-IN",
		"MYUTILS-WG-DNS",
		`dig +time=2 +tries=1 +short @"$WIREGUARD_DNS_RESOLVER_ADDRESS" example.com A`,
		`routing_table_routes="$(ip -4 route show table 51889)"`,
		`^10\.89\.0\.0/24 dev wg-users([[:space:]]|$)`,
		"routingHealthy",
		"MYUTILS-WG-TRAFFIC",
		"routingTraffic",
		"ruDownloadBytes",
		"nonRuUploadBytes",
		"routeQuality: $routeQuality[0]",
		"packetLossPercent",
		`awg show "$active_awg_interface" endpoints`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("wireguard-agent.sh does not contain %q", want)
		}
	}
	if strings.Contains(script, "routingStatus.token") {
		t.Fatal("heartbeat must not expose routingStatus.token")
	}
	if strings.Contains(script, `[[ ! -d "/sys/class/net/$WIREGUARD_AWG_INTERFACE" ]]`) {
		t.Fatal("agent must keep reporting health while the primary AWG interface is down")
	}

	timer := readFile(t, "systemd/my-utils-wireguard-agent.timer")
	if !strings.Contains(timer, "OnUnitActiveSec=15s") {
		t.Fatal("WireGuard timer no longer refreshes counters every 15 seconds")
	}
	agentUnit := readFile(t, "systemd/my-utils-wireguard-agent.service")
	if !strings.Contains(agentUnit, "ReadWritePaths=/run /var/lib/my-utils-wireguard") {
		t.Fatal("WireGuard agent sandbox must permit its managed preference state")
	}
}

func TestRouteProbeUsesQuorumWhenOneTargetStopsAnswering(t *testing.T) {
	tempDir := t.TempDir()
	writeExecutable(t, tempDir+"/ping", `#!/bin/sh
target=""
for argument in "$@"; do target="$argument"; done
case "$target" in
  77.88.8.8)
    printf '%s\n' '3 packets transmitted, 0 received, 100% packet loss, time 2000ms'
    exit 1
    ;;
  77.88.8.1)
    printf '%s\n' '3 packets transmitted, 3 received, 0% packet loss, time 2000ms'
    printf '%s\n' 'rtt min/avg/max/mdev = 2.000/2.500/3.000/0.100 ms'
    ;;
  1.1.1.1)
    printf '%s\n' '3 packets transmitted, 3 received, 0% packet loss, time 2000ms'
    printf '%s\n' 'rtt min/avg/max/mdev = 3.000/4.000/5.000/0.100 ms'
    ;;
  *) exit 2 ;;
esac
`)

	command := exec.Command("bash", "route-probe.sh", "eth0", "77.88.8.8,77.88.8.1,1.1.1.1")
	command.Env = append(os.Environ(), "PATH="+tempDir+":"+os.Getenv("PATH"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("route probe failed: %v\n%s", err, output)
	}
	var probe struct {
		Target            string   `json:"target"`
		PacketLossPercent float64  `json:"packetLossPercent"`
		AverageRTTMs      *float64 `json:"averageRttMs"`
	}
	if err := json.Unmarshal(output, &probe); err != nil {
		t.Fatalf("invalid route probe %q: %v", output, err)
	}
	if probe.Target != "1.1.1.1" || probe.PacketLossPercent != 0 || probe.AverageRTTMs == nil || *probe.AverageRTTMs != 4 {
		t.Fatalf("route probe = %+v, want quorum result from healthy targets", probe)
	}
}

func TestRouteProbeRejectsDuplicateTargets(t *testing.T) {
	tempDir := t.TempDir()
	writeExecutable(t, tempDir+"/ping", `#!/bin/sh
printf '%s\n' '3 packets transmitted, 3 received, 0% packet loss, time 2000ms'
printf '%s\n' 'rtt min/avg/max/mdev = 2.000/2.500/3.000/0.100 ms'
`)
	command := exec.Command("bash", "route-probe.sh", "eth0", "77.88.8.1,77.88.8.1,1.1.1.1")
	command.Env = append(os.Environ(), "PATH="+tempDir+":"+os.Getenv("PATH"))
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "targets must be distinct") {
		t.Fatalf("duplicate targets were accepted: err=%v output=%q", err, output)
	}
}

func TestGeoPrefixRendererProducesDeterministicAtomicTransaction(t *testing.T) {
	output, err := run(t, "31.13.24.0/21\n5.255.192.0/18\n5.255.192.0/18\n", "python3", "render-geo-prefixes.py", "--minimum-prefixes", "2", "--maximum-prefixes", "10")
	if err != nil {
		t.Fatalf("renderer failed: %v\n%s", err, output)
	}
	want := "flush set ip myutils_wg_geo ru_ipv4\n" +
		"add element ip myutils_wg_geo ru_ipv4 { 5.255.192.0/18, 31.13.24.0/21 }\n"
	if output != want {
		t.Fatalf("renderer output = %q, want %q", output, want)
	}
}

func TestGeoPrefixRendererRejectsUnsafeRanges(t *testing.T) {
	for _, unsafe := range []string{"0.0.0.0/0", "10.0.0.0/8", "127.0.0.0/8", "224.0.0.0/4"} {
		output, err := run(t, unsafe+"\n", "python3", "render-geo-prefixes.py", "--minimum-prefixes", "1", "--maximum-prefixes", "10")
		if err == nil {
			t.Errorf("renderer accepted unsafe range %q", unsafe)
		}
		if !strings.Contains(output, "unsafe IPv4 network") {
			t.Errorf("renderer output for %q = %q", unsafe, output)
		}
	}
}

func TestGeoInstallerPlanIsExplicitAndNonMutating(t *testing.T) {
	output, err := run(t, "", "bash", "install-geo-routing.sh", "--client-cidr", "10.89.0.0/24", "--ingress-interface", "wg-users", "--direct-egress-interface", "eth0")
	if err != nil {
		t.Fatalf("installer plan failed: %v\n%s", err, output)
	}
	for _, want := range []string{"Plan only; no host changes were made", "priority 1088", "unmarked traffic remains on AWG table 51889"} {
		if !strings.Contains(output, want) {
			t.Errorf("installer output does not contain %q: %s", want, output)
		}
	}
}

func TestClientDNSInstallerKeepsExistingProfilesIndependentFromAWG(t *testing.T) {
	installer := readFile(t, "install-client-dns.sh")
	for _, want := range []string{
		"Plan only; no host changes were made",
		"dnsmasq",
		"listen-address=$resolver_address",
		"interface=$ingress_interface",
		"After=wg-quick@$ingress_interface.service",
		"server=77.88.8.8",
		"server=1.1.1.1",
		"dnsmasq --test",
		"my-utils-wireguard-dns.service",
	} {
		if !strings.Contains(installer, want) {
			t.Errorf("client DNS installer does not contain %q", want)
		}
	}

	routing := readFile(t, "client-dns.sh")
	for _, want := range []string{
		"MYUTILS-WG-DNS",
		"MYUTILS-WG-DNS-IN",
		`-i "$ingress_interface" -s "$client_cidr" -p udp --dport 53`,
		`-i "$ingress_interface" -s "$client_cidr" -p tcp --dport 53`,
		`DNAT --to-destination "$resolver_address:53"`,
		`-i "$ingress_interface" -s "$client_cidr" -d "$resolver_address" -p udp --dport 53 -j ACCEPT`,
		`-i "$ingress_interface" -s "$client_cidr" -d "$resolver_address" -p tcp --dport 53 -j ACCEPT`,
		`iptables -C INPUT -j "$input_chain"`,
		`iptables -D INPUT -j "$input_chain"`,
	} {
		if !strings.Contains(routing, want) {
			t.Errorf("client DNS routing does not contain %q", want)
		}
	}
	if strings.Contains(routing, "0.0.0.0/0") {
		t.Fatal("client DNS interception must be limited to the WireGuard client CIDR")
	}

	unit := readFile(t, "systemd/my-utils-wireguard-dns.service")
	for _, want := range []string{"Requires=wg-quick@wg-users.service dnsmasq.service", "ExecStart=/usr/local/libexec/my-utils-wireguard-dns start", "ExecStop=/usr/local/libexec/my-utils-wireguard-dns stop"} {
		if !strings.Contains(unit, want) {
			t.Errorf("client DNS unit does not contain %q", want)
		}
	}
}

func TestAPIProxyRoutingIsLimitedToTheConfiguredProxy(t *testing.T) {
	script := readFile(t, "api-proxy-routing.sh")
	for _, want := range []string{
		"docker_cidr=172.16.0.0/12",
		"proxy_destination=185.242.106.81/32",
		"tunnel_proxy_destination=172.29.172.3",
		"proxy_port=8888",
		"egress_interface_pattern=awg-exit+",
		"source_address=10.89.0.1",
		"priority=1087",
		"mark=0x51891",
		"lookup \"$table\"",
		"DNAT --to-destination \"$tunnel_proxy_destination:$proxy_port\"",
		"SNAT --to-source \"$source_address\"",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("api-proxy-routing.sh does not contain %q", want)
		}
	}
	if strings.Contains(script, "0.0.0.0/0") {
		t.Fatal("API proxy routing must not claim a default source or destination route")
	}

	unit := readFile(t, "systemd/my-utils-api-proxy-routing.service")
	for _, want := range []string{
		"Requires=my-utils-wireguard-routing.service",
		"ExecStart=/usr/local/libexec/my-utils-api-proxy-routing start",
		"ExecStop=/usr/local/libexec/my-utils-api-proxy-routing stop",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("proxy routing unit does not contain %q", want)
		}
	}
}

func TestRelayAndAPIProxyRoutingAcceptManagedExitPool(t *testing.T) {
	relayInstaller := readFile(t, "install-relay.sh")
	for _, want := range []string{
		"egress_pattern='awg-exit+'",
		`ip route replace "\$client_cidr" dev "\$ingress" table "\$table" scope link`,
		`-o "\$egress_pattern"`,
		`-i "\$egress_pattern"`,
		"Wants=my-utils-awg-exit.service",
	} {
		if !strings.Contains(relayInstaller, want) {
			t.Errorf("relay installer does not contain %q", want)
		}
	}
	if strings.Contains(relayInstaller, "Requires=my-utils-awg-exit.service") {
		t.Fatal("relay routing must remain active while an individual exit service is down")
	}

	apiProxy := readFile(t, "api-proxy-routing.sh")
	for _, want := range []string{
		"egress_interface_pattern=awg-exit+",
		`-o "$egress_interface_pattern"`,
		`grep -Eq '^default dev awg-exit([[:alnum:]_.-]*) '`,
	} {
		if !strings.Contains(apiProxy, want) {
			t.Errorf("API proxy routing does not contain %q", want)
		}
	}
}

func TestVeespExitComposeKeepsTinyproxyPrivate(t *testing.T) {
	compose := readFile(t, "veesp-exit/compose.yml")
	for _, want := range []string{
		"name: my-utils-awg-exit",
		"com.docker.network.bridge.name: amn0",
		"subnet: 172.29.172.0/24",
		"ipv4_address: 172.29.172.2",
		"ipv4_address: 172.29.172.3",
		`"42697:42697/udp"`,
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("veesp exit compose does not contain %q", want)
		}
	}

	tinyproxyBlock, _, found := strings.Cut(compose, "  tinyproxy:")
	if !found {
		t.Fatal("tinyproxy service is missing")
	}
	_ = tinyproxyBlock
	tinyproxyBlock = "  tinyproxy:" + strings.SplitN(compose, "  tinyproxy:", 2)[1]
	if strings.Contains(tinyproxyBlock, "ports:") {
		t.Fatal("tinyproxy must not publish a host port")
	}
}

func TestVeespExitDockerBuildContextExcludesSecrets(t *testing.T) {
	ignore := readFile(t, "veesp-exit/.dockerignore")
	for _, want := range []string{"*", "!Dockerfile.awg", "!Dockerfile.tinyproxy", "!awg-entrypoint.sh", "!tinyproxy.conf"} {
		if !strings.Contains(ignore, want) {
			t.Errorf("Veesp exit .dockerignore does not contain %q", want)
		}
	}
	installer := readFile(t, "veesp-exit/install.sh")
	if !strings.Contains(installer, ".dockerignore") {
		t.Fatal("Veesp exit installer must install .dockerignore")
	}
}

func TestVeespExitInstallerRejectsForeignDockerState(t *testing.T) {
	installer := readFile(t, "veesp-exit/install.sh")
	for _, want := range []string{
		"Plan only; no host changes were made",
		"Refusing foreign container",
		"Refusing foreign Docker network",
		"mode 600",
		"docker compose config --quiet",
	} {
		if !strings.Contains(installer, want) {
			t.Errorf("veesp exit installer does not contain %q", want)
		}
	}
}

func TestVeespExitEntrypointOwnsOnlyContainerRules(t *testing.T) {
	entrypoint := readFile(t, "veesp-exit/awg-entrypoint.sh")
	for _, want := range []string{
		"MYUTILS_AWG_FORWARD",
		"MYUTILS_AWG_NAT",
		"10.8.1.250/32",
		"10.89.0.0/24",
		"172.29.172.3",
		"MASQUERADE",
	} {
		if !strings.Contains(entrypoint, want) {
			t.Errorf("AWG entrypoint does not contain %q", want)
		}
	}
	if strings.Contains(entrypoint, "nsenter") || strings.Contains(entrypoint, "--network host") {
		t.Fatal("AWG entrypoint must not enter the host network namespace")
	}
}

func TestVeespExitEntrypointPrefersKernelAmneziaWG(t *testing.T) {
	entrypoint := readFile(t, "veesp-exit/awg-entrypoint.sh")
	for _, want := range []string{
		`ip link add "$interface" type amneziawg`,
		`mkdir -p /run/amneziawg`,
		`amneziawg-go -f "$interface"`,
	} {
		if !strings.Contains(entrypoint, want) {
			t.Errorf("AWG entrypoint does not contain %q", want)
		}
	}
}

func TestVeespExitGeneratorKeepsSecretsInProtectedFiles(t *testing.T) {
	generator := readFile(t, "veesp-exit/generate-config.sh")
	for _, want := range []string{
		"umask 077",
		"wg genkey",
		"wg genpsk",
		"chmod 600",
		"Generated protected server and client parameter files",
	} {
		if !strings.Contains(generator, want) {
			t.Errorf("veesp exit generator does not contain %q", want)
		}
	}
	if strings.Contains(generator, "cat \"$server_private\"") || strings.Contains(generator, "cat \"$preshared_key\"") {
		t.Fatal("generator must not print private key material")
	}
}

func TestExitHostBootstrapHardensSSHAndBoundsResourceUse(t *testing.T) {
	bootstrap := readFile(t, "veesp-exit/bootstrap-host.sh")
	for _, want := range []string{
		"Plan only; no host changes were made",
		"docker-compose-v2",
		"fail2ban",
		"PasswordAuthentication no",
		"PermitRootLogin prohibit-password",
		"MaxAuthTries 10",
		"bantime.increment = true",
		"mkswap",
		"SystemMaxUse=200M",
		`"max-size": "20m"`,
		"sshd -t",
	} {
		if !strings.Contains(bootstrap, want) {
			t.Errorf("exit bootstrap does not contain %q", want)
		}
	}
}

func TestVeespExitGeneratorSupportsIndependentOverlaySubnet(t *testing.T) {
	tempDir := t.TempDir()
	clientPublic := tempDir + "/client.pub"
	serverConfig := tempDir + "/awg0.conf"
	clientParams := tempDir + "/client.params"
	if err := os.WriteFile(clientPublic, []byte("Q0xJRU5UX1BVQkxJQ19LRVlfMDAwMDAwMDAwMDAwMDA=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, tempDir+"/wg", `#!/bin/sh
case "$1" in
  genkey) printf '%s\n' 'U0VSVkVSX1BSSVZBVEVfS0VZXzAwMDAwMDAwMDAwMDA=' ;;
  pubkey) cat >/dev/null; printf '%s\n' 'U0VSVkVSX1BVQkxJQ19LRVlfMDAwMDAwMDAwMDAwMDA=' ;;
  genpsk) printf '%s\n' 'UFJFU0hBUkVEX0tFWV8wMDAwMDAwMDAwMDAwMDAwMDA=' ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, tempDir+"/shuf", `#!/bin/sh
printf '%s\n' "${2%%-*}"
`)

	command := exec.Command("bash", "veesp-exit/generate-config.sh",
		"--client-public-key-file", clientPublic,
		"--server-config", serverConfig,
		"--client-params", clientParams,
		"--endpoint", "153.76.223.117:42697",
		"--server-address", "10.8.2.1/24",
		"--client-address", "10.8.2.250/32",
	)
	command.Env = append(os.Environ(), "PATH="+tempDir+":"+os.Getenv("PATH"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generator failed: %v\n%s", err, output)
	}
	server := readPath(t, serverConfig)
	if !strings.Contains(server, "Address = 10.8.2.1/24") || !strings.Contains(server, "AllowedIPs = 10.8.2.250/32, 10.89.0.0/24") {
		t.Fatalf("server config does not use the requested overlay:\n%s", server)
	}
	params := readPath(t, clientParams)
	if !strings.Contains(params, "CLIENT_ADDRESS=10.8.2.250/32") {
		t.Fatalf("client params do not preserve the requested address:\n%s", params)
	}
	for _, path := range []string{serverConfig, clientParams} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", path, info.Mode().Perm())
		}
	}
}

func TestVeespExitClientSwitchIsAtomicAndRollsBack(t *testing.T) {
	switcher := readFile(t, "veesp-exit/switch-utils-client.sh")
	for _, want := range []string{
		"umask 077",
		`staging="$staging_dir/awg-exit.conf"`,
		"awg-quick strip",
		"rollback()",
		"systemctl stop my-utils-awg-exit.service",
		"systemctl start my-utils-awg-exit.service",
		"capture_active_routing_units",
		"restore_active_routing_units",
		"latest-handshakes",
		"expected_egress",
	} {
		if !strings.Contains(switcher, want) {
			t.Errorf("Veesp client switcher does not contain %q", want)
		}
	}
}

func TestUtilsExitClientInstallerKeepsReserveOutsidePolicyRouting(t *testing.T) {
	installer := readFile(t, "veesp-exit/install-utils-client.sh")
	for _, want := range []string{
		"Plan only; no host changes were made",
		"CLIENT_ADDRESS",
		"Table = off",
		"systemctl enable --now",
		"latest-handshakes",
		"expected_egress",
		"AWG client $interface is healthy and not selected for policy routing",
	} {
		if !strings.Contains(installer, want) {
			t.Errorf("utils exit client installer does not contain %q", want)
		}
	}
	if strings.Contains(installer, "ip route replace") {
		t.Fatal("AWG client installer must not select itself in the policy table")
	}
}

func TestUtilsExitClientInstallerSupportsPrimaryAndSecondaryInterfaces(t *testing.T) {
	installer := readFile(t, "veesp-exit/install-utils-client.sh")
	for _, want := range []string{
		"Plan: install and validate AWG interface $interface without changing policy table 51889",
		"AWG client $interface is healthy and not selected for policy routing",
		"Description=my-utils AmneziaWG egress ($interface)",
	} {
		if !strings.Contains(installer, want) {
			t.Errorf("generic utils exit client installer does not contain %q", want)
		}
	}
	if strings.Contains(installer, `"$interface" != awg-exit`) {
		t.Fatal("generic utils exit client installer still rejects the primary interface")
	}
}

func TestRelayInstallerCanReuseProtectedServerPrivateKey(t *testing.T) {
	installer := readFile(t, "install-relay.sh")
	for _, want := range []string{
		"--server-private-key-file",
		"WireGuard server private key file must exist with mode 600",
		`PrivateKey = $server_private_key`,
		"unset server_private_key",
	} {
		if !strings.Contains(installer, want) {
			t.Errorf("relay installer does not contain %q", want)
		}
	}
	if strings.Contains(installer, "cat \"$server_private_key_file\"") {
		t.Fatal("relay installer must not print the supplied private key")
	}
}

func TestUtilsHostPreparationIsPlanOnlyAndInstallsAWGTooling(t *testing.T) {
	installer := readFile(t, "veesp-exit/prepare-utils-host.sh")
	info, err := os.Stat("veesp-exit/prepare-utils-host.sh")
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("utils host preparation script must be executable: %v", err)
	}
	for _, want := range []string{
		"mode=plan",
		"Plan only; no host changes were made",
		"ppa:amnezia/ppa",
		"amneziawg",
		"install -d -m 700 /etc/my-utils/awg-identities /etc/my-utils/awg-params",
	} {
		if !strings.Contains(installer, want) {
			t.Errorf("utils host preparation does not contain %q", want)
		}
	}
}

func TestWireGuardAnsibleBundleKeepsSecretsOffControllerDisk(t *testing.T) {
	playbook := readFile(t, "ansible/site.yml")
	stageFiles := readFile(t, "ansible/stage-files.txt")
	inventory := readFile(t, "ansible/inventory.example.yml")
	vault := readFile(t, "ansible/vault.example.yml")
	validator := readFile(t, "ansible/validate.py")
	for _, want := range []string{
		"vpn_apply | bool",
		"vault_wireguard_agent_token",
		"vault_wireguard_server_private_key",
		"vault_awg_client_private_keys",
		"no_log: true",
		"ansible.builtin.slurp",
		"my-utils-awg-failover.service",
		"my-utils-wireguard-dns.service",
	} {
		if !strings.Contains(playbook, want) {
			t.Errorf("Ansible playbook does not contain %q", want)
		}
	}
	for _, forbidden := range []string{"ansible.builtin.fetch", "local_action", "delegate_to: localhost"} {
		if strings.Contains(playbook, forbidden) {
			t.Errorf("Ansible playbook writes or delegates secret material through the controller: %q", forbidden)
		}
	}
	for _, forbidden := range []string{"ansible/", "vault", "client.params", "awg0.conf"} {
		if strings.Contains(stageFiles, forbidden) {
			t.Errorf("Ansible staging whitelist contains secret-capable path %q", forbidden)
		}
	}
	for _, path := range strings.Fields(stageFiles) {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Errorf("Ansible staging whitelist path %q is not a regular source file: %v", path, err)
		}
	}
	for _, want := range []string{"stage-files.txt", "Stage only whitelisted WireGuard operation files", "state: absent"} {
		if !strings.Contains(playbook, want) {
			t.Errorf("Ansible playbook does not enforce clean whitelisted staging: %q", want)
		}
	}
	for _, want := range []string{"vpn_relay:", "vpn_exits:", "awg_overlay_octet:", "expected_egress:"} {
		if !strings.Contains(inventory, want) {
			t.Errorf("Ansible example inventory does not contain %q", want)
		}
	}
	for _, want := range []string{"vault_wireguard_agent_token:", "vault_wireguard_server_private_key:", "vault_awg_client_private_keys:"} {
		if !strings.Contains(vault, want) {
			t.Errorf("Ansible vault example does not contain %q", want)
		}
	}
	if !strings.Contains(validator, "exactly two vpn_exits hosts are required") {
		t.Fatal("Ansible validator does not enforce dual-exit topology")
	}
}

func TestVPNAlertProvisioningCoversRelayAndBothExits(t *testing.T) {
	rules := readFile(t, "../../observability/config/grafana/provisioning/alerting/vpn-alert-rules.yaml")
	template := readFile(t, "../../observability/config/grafana/provisioning/alerting/metal-templates.yaml")
	validator := readFile(t, "../../observability/scripts/validate-vpn-alerts.py")
	for _, want := range []string{
		"VPN metrics unavailable",
		"VPN relay unavailable",
		"VPN agent stale",
		"VPN routing unhealthy",
		"VPN all exits down",
		"VPN primary exit unhealthy",
		"VPN running on reserve",
		"VPN packet loss high",
		"receiver: Metal Discord",
		"myutils_wireguard_collection_success",
		"myutils_wireguard_exit_healthy",
		"myutils_wireguard_exit_selected",
		"myutils_wireguard_exit_preference",
		"myutils_wireguard_route_packet_loss_percent",
	} {
		if !strings.Contains(rules, want) {
			t.Errorf("VPN alert provisioning does not contain %q", want)
		}
	}
	for _, want := range []string{`eq .Labels.team "vpn"`, "VPN alert", "Open VPN"} {
		if !strings.Contains(template, want) {
			t.Errorf("shared alert template does not contain %q", want)
		}
	}
	if !strings.Contains(validator, "EXPECTED_ALERTS") || !strings.Contains(validator, "repeat_interval") {
		t.Fatal("VPN alert validator does not enforce the provisioned rule contract")
	}
}

func TestUtilsExitClientIdentityGeneratorProtectsPrivateKey(t *testing.T) {
	generator := readFile(t, "veesp-exit/generate-utils-client-identity.sh")
	for _, want := range []string{"umask 077", "awg genkey", "awg pubkey", "chmod 600"} {
		if !strings.Contains(generator, want) {
			t.Errorf("utils client identity generator does not contain %q", want)
		}
	}
	if strings.Contains(generator, "cat \"$private_key_file\"") {
		t.Fatal("identity generator must not print the private key")
	}
}

func TestAWGFailoverDecisionUsesHysteresisAndFailClosed(t *testing.T) {
	state := `{"active":"primary","counters":{}}`
	for attempt := 1; attempt <= 3; attempt++ {
		state = runFailoverDecision(t, state, false, true)
		want := "primary"
		if attempt == 3 {
			want = "secondary"
		}
		if got := failoverActive(t, state); got != want {
			t.Fatalf("active after primary failure %d = %q, want %q", attempt, got, want)
		}
	}
	for attempt := 1; attempt <= 2; attempt++ {
		state = runFailoverDecision(t, state, true, true)
		want := "secondary"
		if attempt == 2 {
			want = "primary"
		}
		if got := failoverActive(t, state); got != want {
			t.Fatalf("active after primary recovery %d = %q, want %q", attempt, got, want)
		}
	}
	for attempt := 1; attempt <= 3; attempt++ {
		state = runFailoverDecision(t, state, false, false)
	}
	if got := failoverActive(t, state); got != "" {
		t.Fatalf("active with both exits down = %q, want fail-closed empty selection", got)
	}
}

func TestAWGFailoverDecisionHonorsManualPreferenceWithSafeFallback(t *testing.T) {
	state := `{"active":"primary","counters":{}}`
	state = runFailoverDecisionWithPreference(t, state, true, true, "SECONDARY")
	if got := failoverActive(t, state); got != "secondary" {
		t.Fatalf("active with healthy preferred secondary = %q, want secondary", got)
	}
	state = runFailoverDecisionWithPreference(t, state, true, false, "SECONDARY")
	if got := failoverActive(t, state); got != "primary" {
		t.Fatalf("active with failed preferred secondary = %q, want safe primary fallback", got)
	}
}

func TestAWGFailoverRunnerUsesEndToEndProbesAndAtomicPolicyRoute(t *testing.T) {
	runner := readFile(t, "veesp-exit/awg-failover.sh")
	for _, want := range []string{
		"latest-handshakes",
		`--interface "$interface"`,
		`ping -n -I "$interface" -c 2 -W 2 "$AWG_LATENCY_TARGET"`,
		"expected_egress",
		`ip route replace default dev "$desired_interface" table "$AWG_ROUTE_TABLE"`,
		`ip route del default dev "$interface" table "$AWG_ROUTE_TABLE"`,
		`ip route replace unreachable default table "$AWG_ROUTE_TABLE"`,
		"flock -n",
		"exit-health.json",
		"overallStatus",
		"AWG_PREFERENCE_FILE",
		`--preference "$preference"`,
	} {
		if !strings.Contains(runner, want) {
			t.Errorf("AWG failover runner does not contain %q", want)
		}
	}

	installer := readFile(t, "veesp-exit/install-awg-failover.sh")
	for _, want := range []string{
		"Plan only; no host changes were made",
		"AWG_PRIMARY_INTERFACE",
		"AWG_SECONDARY_INTERFACE",
		"AWG_LATENCY_TARGET=1.1.1.1",
		"AWG_PREFERENCE_FILE=/var/lib/my-utils-wireguard/exit-preference",
		"chmod 600",
		"systemctl enable --now my-utils-awg-failover.timer",
		"systemctl start my-utils-awg-failover.service",
		"systemctl enable --now my-utils-api-proxy-routing.service",
		"my-utils-api-proxy-routing.service",
	} {
		if !strings.Contains(installer, want) {
			t.Errorf("AWG failover installer does not contain %q", want)
		}
	}

	timer := readFile(t, "veesp-exit/my-utils-awg-failover.timer")
	if !strings.Contains(timer, "OnUnitActiveSec=5s") || !strings.Contains(timer, "AccuracySec=1s") {
		t.Fatal("AWG failover timer must evaluate health every five seconds")
	}
	failoverUnit := readFile(t, "veesp-exit/my-utils-awg-failover.service")
	for _, line := range strings.Split(failoverUnit, "\n") {
		if (strings.HasPrefix(line, "Wants=") || strings.HasPrefix(line, "Requires=")) && strings.Contains(line, "my-utils-awg-exit") {
			t.Fatal("health checks must observe stopped exit services without restarting them")
		}
	}
}

func TestAWGFailoverRunnerTreatsVanishedInterfaceAsFailedProbe(t *testing.T) {
	runner := readFile(t, "veesp-exit/awg-failover.sh")
	if !strings.Contains(runner, `if ! handshake_output=$(awg show "$interface" latest-handshakes 2>/dev/null); then`) {
		t.Fatal("AWG probe must tolerate an interface disappearing between the link check and handshake read")
	}
	if strings.Contains(runner, `handshake=$(awg show "$interface" latest-handshakes`) {
		t.Fatal("an unguarded AWG handshake read aborts the whole failover cycle when an exit is down")
	}
	if !strings.Contains(runner, `if ip link show dev "$desired_interface" >/dev/null 2>&1; then`) {
		t.Fatal("route hysteresis must stay fail-closed while the selected interface is absent")
	}
	if !strings.Contains(runner, `'[.primary.healthy, .secondary.healthy] | all'`) {
		t.Fatal("overall exit health must stay degraded while either provider is down")
	}
}

func TestHAWireGuardRoutingPreservesManagedSelectionAndAllowsBothExits(t *testing.T) {
	routing := readFile(t, "veesp-exit/wireguard-routing-ha.sh")
	for _, want := range []string{
		"WIREGUARD_EXIT_PATTERN",
		`ip route replace "$WIREGUARD_CLIENT_CIDR" dev "$WIREGUARD_INGRESS_INTERFACE" table "$WIREGUARD_ROUTE_TABLE" scope link`,
		`-o "$WIREGUARD_EXIT_PATTERN"`,
		`-i "$WIREGUARD_EXIT_PATTERN"`,
		"current_managed_default",
		`ip route replace default dev "$WIREGUARD_PRIMARY_EXIT"`,
	} {
		if !strings.Contains(routing, want) {
			t.Errorf("HA WireGuard routing does not contain %q", want)
		}
	}
	unit := readFile(t, "veesp-exit/my-utils-wireguard-routing-ha.service")
	if strings.Contains(unit, "Requires=my-utils-awg-exit") {
		t.Fatal("HA routing unit must survive either exit service stopping")
	}
	for _, want := range []string{"Wants=my-utils-awg-exit.service my-utils-awg-exit-b.service", "Requires=wg-quick@wg-users.service"} {
		if !strings.Contains(unit, want) {
			t.Errorf("HA routing unit does not contain %q", want)
		}
	}
}

func TestRoutingUnitStateCapturesOnlyActiveDependents(t *testing.T) {
	tempDir := t.TempDir()
	activeFile := tempDir + "/active"
	stateFile := tempDir + "/state"
	logFile := tempDir + "/systemctl.log"
	writeFakeSystemctl(t, tempDir)
	if err := os.WriteFile(activeFile, []byte("my-utils-wireguard-routing.service\nmy-utils-geo-routing.service\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("bash", "-c", `source "$1"; capture_active_routing_units "$2"`, "bash", "veesp-exit/routing-units.sh", stateFile)
	command.Env = append(os.Environ(), "PATH="+tempDir+":"+os.Getenv("PATH"), "ACTIVE_UNITS_FILE="+activeFile, "SYSTEMCTL_LOG="+logFile)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("capture failed: %v\n%s", err, output)
	}
	state, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "my-utils-wireguard-routing.service\nmy-utils-geo-routing.service\n"
	if string(state) != want {
		t.Fatalf("captured state = %q, want %q", state, want)
	}
}

func TestRoutingUnitStateRestoresDependentsInDependencyOrder(t *testing.T) {
	tempDir := t.TempDir()
	activeFile := tempDir + "/active"
	stateFile := tempDir + "/state"
	logFile := tempDir + "/systemctl.log"
	writeFakeSystemctl(t, tempDir)
	state := "my-utils-wireguard-routing.service\nmy-utils-geo-routing.service\nmy-utils-api-proxy-routing.service\n"
	if err := os.WriteFile(activeFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFile, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("bash", "-c", `source "$1"; restore_active_routing_units "$2"`, "bash", "veesp-exit/routing-units.sh", stateFile)
	command.Env = append(os.Environ(), "PATH="+tempDir+":"+os.Getenv("PATH"), "ACTIVE_UNITS_FILE="+activeFile, "SYSTEMCTL_LOG="+logFile)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("restore failed: %v\n%s", err, output)
	}
	log, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "start my-utils-wireguard-routing.service\n" +
		"start my-utils-geo-routing.service\n" +
		"start my-utils-geo-routing-update.service\n" +
		"start my-utils-api-proxy-routing.service\n"
	if string(log) != want {
		t.Fatalf("systemctl calls = %q, want %q", log, want)
	}
}

func TestVeespExitVelocitySwitchAvoidsInterfaceDowntime(t *testing.T) {
	switcher := readFile(t, "veesp-exit/switch-velocity-proxy.sh")
	for _, want := range []string{
		"wg syncconf wg-utils",
		"172.29.172.1",
		"172.29.172.3",
		"rollback()",
		"expected_egress",
	} {
		if !strings.Contains(switcher, want) {
			t.Errorf("Velocity proxy switcher does not contain %q", want)
		}
	}
	if strings.Contains(switcher, "wg-quick down") {
		t.Fatal("Velocity switcher must not take wg-utils down")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func readPath(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
}

func runFailoverDecision(t *testing.T, state string, primaryHealthy, secondaryHealthy bool) string {
	return runFailoverDecisionWithPreference(t, state, primaryHealthy, secondaryHealthy, "AUTO")
}

func runFailoverDecisionWithPreference(t *testing.T, state string, primaryHealthy, secondaryHealthy bool, preference string) string {
	t.Helper()
	tempDir := t.TempDir()
	stateFile := tempDir + "/state.json"
	probesFile := tempDir + "/probes.json"
	if err := os.WriteFile(stateFile, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	probes := fmt.Sprintf(`{"primary":{"healthy":%t},"secondary":{"healthy":%t}}`, primaryHealthy, secondaryHealthy)
	if err := os.WriteFile(probesFile, []byte(probes), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", "veesp-exit/decide-failover.py",
		"--state", stateFile,
		"--probes", probesFile,
		"--failure-threshold", "3",
		"--recovery-threshold", "2",
		"--preference", preference,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("failover decision failed: %v\n%s", err, output)
	}
	return string(output)
}

func failoverActive(t *testing.T, state string) string {
	t.Helper()
	var result struct {
		Active *string `json:"active"`
	}
	if err := json.Unmarshal([]byte(state), &result); err != nil {
		t.Fatalf("invalid failover state %q: %v", state, err)
	}
	if result.Active == nil {
		return ""
	}
	return *result.Active
}

func writeFakeSystemctl(t *testing.T, dir string) {
	t.Helper()
	path := dir + "/systemctl"
	contents := `#!/bin/sh
set -eu
if [ "$1" = "is-active" ] && [ "$2" = "--quiet" ]; then
  grep -Fxq "$3" "$ACTIVE_UNITS_FILE"
  exit $?
fi
printf '%s\n' "$*" >>"$SYSTEMCTL_LOG"
`
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, input string, name string, args ...string) (string, error) {
	t.Helper()
	command := exec.Command(name, args...)
	command.Stdin = strings.NewReader(input)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	return output.String(), err
}
