package batch

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

func stranded(mac, addr string) *mhed.Device {
	return &mhed.Device{MAC: mac, IP: addr, Mask: "255.0.0.0", Gateway: "10.0.0.1", DNS1: "10.0.0.1", DNS2: "8.8.8.8", Name: "lost"}
}

var home = RescueInput{Subnet: netip.MustParsePrefix("192.168.1.145/24"), Host: ip("192.168.1.145")}

func targets(t *testing.T, reqs []Request) []string {
	t.Helper()
	out := make([]string, len(reqs))
	for i, r := range reqs {
		if r.IP.IsValid() {
			out[i] = r.IP.String()
		}
	}
	return out
}

// The usual case: some cameras already live on the network; the stranded ones join them right after the highest,
// with the mask of the subnet and the gateway the others use.
func TestRescueJoinsTheCamerasAlreadyThere(t *testing.T) {
	devs := fleet(dev(macA, "192.168.1.20"), dev(macB, "192.168.1.21"), stranded(macC, "10.0.0.5"), stranded("02:00:00:00:00:0d", "10.0.0.5"))
	reqs, assumed, err := Rescue(devs, []string{macC, "02:00:00:00:00:0d"}, home)
	if err != nil || assumed {
		t.Fatalf("err %v, gateway assumed %v", err, assumed)
	}
	if got := strings.Join(targets(t, reqs), " "); got != "192.168.1.22 192.168.1.23" {
		t.Errorf("addresses %q, want 192.168.1.22 192.168.1.23", got)
	}
	for _, r := range reqs {
		if r.Mask != ip("255.255.255.0") || r.Gateway != ip("192.168.1.1") || r.DNS1 != ip("192.168.1.1") || r.DNS2.IsValid() {
			t.Errorf("%s: mask %v gw %v dns1 %v dns2 %v", r.MAC, r.Mask, r.Gateway, r.DNS1, r.DNS2)
		}
	}
	// And the plan built from it is a plain, valid batch.
	items := Build(devs, reqs)
	if Blocked(items) || Count(items, Planned) != 2 {
		t.Fatalf("plan: %+v", items)
	}
	if items[0].Target.DNS2 != ip("8.8.8.8") {
		t.Errorf("DNS 2 was not kept: %v", items[0].Target.DNS2)
	}
}

func TestRescueSkipsAddressesInUse(t *testing.T) {
	devs := fleet(dev(macA, "192.168.1.143"), stranded(macB, "10.0.0.5"), stranded(macC, "10.0.0.6"))
	in := home
	in.Taken = map[netip.Addr]bool{ip("192.168.1.146"): true} // a printer, say
	reqs, _, err := Rescue(devs, []string{macB, macC}, in)
	if err != nil {
		t.Fatal(err)
	}
	// .144 is free, .145 is this computer, .146 is taken, .147 is free.
	if got := strings.Join(targets(t, reqs), " "); got != "192.168.1.144 192.168.1.147" {
		t.Errorf("addresses %q, want 192.168.1.144 192.168.1.147", got)
	}
}

func TestRescueWrapsAroundAndRunsOut(t *testing.T) {
	small := RescueInput{Subnet: netip.MustParsePrefix("192.168.1.0/29"), Host: ip("192.168.1.2")} // hosts .1 – .6
	devs := fleet(dev(macA, "192.168.1.5"), stranded(macB, "10.0.0.5"), stranded(macC, "10.0.0.6"), stranded("02:00:00:00:00:0d", "10.0.0.7"))
	// Gateway copied from A is .1, host is .2, A is .5: free are .6, then (wrapping) .3 and .4.
	reqs, _, err := Rescue(devs, []string{macB, macC, "02:00:00:00:00:0d"}, small)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(targets(t, reqs), " "); got != "192.168.1.6 192.168.1.3 192.168.1.4" {
		t.Errorf("addresses %q, want 192.168.1.6 192.168.1.3 192.168.1.4", got)
	}
	devs["02:00:00:00:00:0e"] = stranded("02:00:00:00:00:0e", "10.0.0.8")
	if _, _, err := Rescue(devs, []string{macB, macC, "02:00:00:00:00:0d", "02:00:00:00:00:0e"}, small); err == nil {
		t.Error("four cameras into three free addresses must fail")
	}
}

// Nothing on the network to learn from: addresses come from the top of the subnet, and the gateway is a guess the
// caller has to be told about.
func TestRescueIntoAnEmptyNetwork(t *testing.T) {
	devs := fleet(stranded(macA, "10.0.0.5"), stranded(macB, "10.0.0.5"))
	reqs, assumed, err := Rescue(devs, []string{macA, macB}, home)
	if err != nil || !assumed {
		t.Fatalf("err %v, gateway assumed %v (want true)", err, assumed)
	}
	if got := strings.Join(targets(t, reqs), " "); got != "192.168.1.254 192.168.1.253" {
		t.Errorf("addresses %q, want 192.168.1.254 192.168.1.253", got)
	}
	if reqs[0].Gateway != ip("192.168.1.1") {
		t.Errorf("assumed gateway %v, want the subnet's first address", reqs[0].Gateway)
	}
	// A gateway given by the caller is used as is, and is not a guess.
	in := home
	in.Gateway, in.DNS1, in.DNS2 = ip("192.168.1.254"), ip("1.1.1.1"), ip("9.9.9.9")
	reqs, assumed, _ = Rescue(devs, []string{macA}, in)
	if assumed || reqs[0].Gateway != ip("192.168.1.254") || reqs[0].DNS1 != ip("1.1.1.1") || reqs[0].DNS2 != ip("9.9.9.9") {
		t.Errorf("explicit gateway/DNS not honoured: assumed %v, %+v", assumed, reqs[0].Config)
	}
	if reqs[0].IP != ip("192.168.1.253") {
		t.Errorf("address %v, want 192.168.1.253 (the gateway's own address is never handed out)", reqs[0].IP)
	}
}

func TestRescueLeavesAloneWhatDoesNotNeedIt(t *testing.T) {
	devs := fleet(dev(macA, "192.168.1.20"), stranded(macB, "10.0.0.5"))
	reqs, _, err := Rescue(devs, []string{macA, "02-00-00-00-00-0B", macC}, home) // on the network, stranded (other spelling), not seen
	if err != nil {
		t.Fatal(err)
	}
	items := Build(devs, reqs)
	for i, want := range []Status{Unchanged, Planned, NotSeen} {
		if items[i].Status != want {
			t.Errorf("item %d: %s, want %s", i, items[i].Status, want)
		}
	}
	// The majority gateway wins when the cameras on the network disagree.
	odd := dev(macC, "192.168.1.30")
	odd.Gateway = "192.168.1.254"
	devs = fleet(dev(macA, "192.168.1.20"), dev("02:00:00:00:00:0d", "192.168.1.21"), odd, stranded(macB, "10.0.0.5"))
	reqs, _, _ = Rescue(devs, []string{macB}, home)
	if reqs[0].Gateway != ip("192.168.1.1") {
		t.Errorf("gateway %v, want the one two of three cameras use", reqs[0].Gateway)
	}
	if _, _, err := Rescue(devs, []string{macB}, RescueInput{Subnet: netip.MustParsePrefix("192.168.1.0/31")}); err == nil {
		t.Error("a /31 has no room and must be refused")
	}
}
