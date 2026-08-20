package wireguardops

import (
	"bytes"
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
	script := readFile(t, "wireguard-agent.sh")
	for _, want := range []string{
		"WIREGUARD_ROUTING_STATUS_FILE",
		"routingStatus: $routingStatus[0]",
		`mode == "RU_DIRECT_AWG_DEFAULT"`,
		"MYUTILS-WG-TRAFFIC",
		"routingTraffic",
		"ruDownloadBytes",
		"nonRuUploadBytes",
		"routeQuality: $routeQuality[0]",
		"packetLossPercent",
		`awg show "$WIREGUARD_AWG_INTERFACE" endpoints`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("wireguard-agent.sh does not contain %q", want)
		}
	}
	if strings.Contains(script, "routingStatus.token") {
		t.Fatal("heartbeat must not expose routingStatus.token")
	}

	timer := readFile(t, "systemd/my-utils-wireguard-agent.timer")
	if !strings.Contains(timer, "OnUnitActiveSec=15s") {
		t.Fatal("WireGuard timer no longer refreshes counters every 15 seconds")
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

func TestAPIProxyRoutingIsLimitedToTheConfiguredProxy(t *testing.T) {
	script := readFile(t, "api-proxy-routing.sh")
	for _, want := range []string{
		"docker_cidr=172.16.0.0/12",
		"proxy_destination=185.242.106.81/32",
		"tunnel_proxy_destination=172.29.172.3",
		"proxy_port=8888",
		"egress_interface=awg-exit",
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

func TestVeespExitClientSwitchIsAtomicAndRollsBack(t *testing.T) {
	switcher := readFile(t, "veesp-exit/switch-utils-client.sh")
	for _, want := range []string{
		"umask 077",
		`staging="$staging_dir/awg-exit.conf"`,
		"awg-quick strip",
		"rollback()",
		"systemctl stop my-utils-awg-exit.service",
		"systemctl start my-utils-awg-exit.service",
		"latest-handshakes",
		"expected_egress",
	} {
		if !strings.Contains(switcher, want) {
			t.Errorf("Veesp client switcher does not contain %q", want)
		}
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
