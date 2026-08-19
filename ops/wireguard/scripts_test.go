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
