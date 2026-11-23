package batch

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

func ip(s string) netip.Addr { return netip.MustParseAddr(s) }

func dev(mac, addr string) *mhed.Device {
	return &mhed.Device{MAC: mac, IP: addr, Mask: "255.255.255.0", Gateway: "192.168.1.1", DNS1: "192.168.1.1",
		DNS2: "8.8.8.8", Name: "cam-" + mac[len(mac)-2:], Model: "DCN-TEST"}
}

func fleet(ds ...*mhed.Device) map[string]*mhed.Device {
	m := map[string]*mhed.Device{}
	for _, d := range ds {
		m[d.MAC] = d
	}
	return m
}

const (
	macA = "02:00:00:00:00:0a"
	macB = "02:00:00:00:00:0b"
	macC = "02:00:00:00:00:0c"
)

// Scenario one: consecutive addresses for a row of cameras; everything else is kept.
func TestBuildSequentialAddresses(t *testing.T) {
	devs := fleet(dev(macA, "192.168.1.88"), dev(macB, "192.168.1.88"), dev(macC, "192.168.1.88")) // factory-fresh, same IP
	seq, err := Sequence(ip("192.168.1.100"), 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	items := Build(devs, []Request{
		{MAC: macA, Config: Config{IP: seq[0]}}, {MAC: macB, Config: Config{IP: seq[1]}}, {MAC: macC, Config: Config{IP: seq[2]}}})
	if Blocked(items) {
		t.Fatalf("blocked: %+v", items)
	}
	for i, want := range []string{"192.168.1.100", "192.168.1.101", "192.168.1.102"} {
		it := items[i]
		if it.Status != Planned || it.Target.IP.String() != want || strings.Join(it.Changes, ",") != "ip" {
			t.Errorf("item %d: status %s target %v changes %v, want planned %s [ip]", i, it.Status, it.Target, it.Changes, want)
		}
		if it.Target.Gateway != ip("192.168.1.1") || it.Target.Mask != ip("255.255.255.0") || it.Target.DNS2 != ip("8.8.8.8") {
			t.Errorf("item %d: unrequested fields were not kept: %+v", i, it.Target)
		}
	}
}

// Scenario two: a new gateway for the whole fleet, addresses untouched.
func TestBuildGatewayInPlace(t *testing.T) {
	devs := fleet(dev(macA, "192.168.1.10"), dev(macB, "192.168.1.11"))
	gw := Config{Gateway: ip("192.168.1.254")}
	items := Build(devs, []Request{{MAC: macA, Config: gw}, {MAC: macB, Config: gw}})
	for i, it := range items {
		if it.Status != Planned || strings.Join(it.Changes, ",") != "gateway" || it.Target.IP != it.Current.IP {
			t.Errorf("item %d: %+v", i, it)
		}
		// DNS 1 was the old gateway and nobody asked to change it: worth saying.
		if len(it.Warnings) != 1 || !strings.Contains(it.Warnings[0], "old gateway") {
			t.Errorf("item %d: warnings %v, want the stale-DNS warning", i, it.Warnings)
		}
	}
	// Asking for DNS 1 as well silences it.
	both := Config{Gateway: ip("192.168.1.254"), DNS1: ip("192.168.1.254")}
	if it := Build(devs, []Request{{MAC: macA, Config: both}})[0]; len(it.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", it.Warnings)
	}
}

func TestBuildStatuses(t *testing.T) {
	devs := fleet(dev(macA, "192.168.1.10"), dev(macB, "192.168.1.11"))
	items := Build(devs, []Request{
		{MAC: macA, Config: Config{IP: ip("192.168.1.10")}}, // same as now
		{MAC: macC, Config: Config{IP: ip("192.168.1.50")}}, // not on the network
		{MAC: "nonsense"}, // not a MAC
		{MAC: macB, Config: Config{IP: ip("192.168.1.255")}},               // broadcast address
		{MAC: macB, Config: Config{IP: ip("192.168.1.60")}},                // second mention of B
		{MAC: "02-00-00-00-00-0A", Config: Config{IP: ip("192.168.1.61")}}, // A again, spelled differently
	})
	want := []Status{Unchanged, NotSeen, Invalid, Invalid, Invalid, Invalid}
	for i, w := range want {
		if items[i].Status != w {
			t.Errorf("item %d: status %s (%s), want %s", i, items[i].Status, items[i].Error, w)
		}
	}
	if !Blocked(items) {
		t.Error("a batch with invalid items must be blocked")
	}
	if Blocked(items[:2]) {
		t.Error("unchanged and not-seen items must not block a batch")
	}
	if items[0].Changes == nil || items[1].Warnings == nil {
		t.Error("Changes and Warnings must be empty slices, not nil, so they encode as [] in JSON")
	}
}

func TestBuildRejectsBadMaskAndWarnsAboutGateway(t *testing.T) {
	devs := fleet(dev(macA, "192.168.1.10"))
	if it := Build(devs, []Request{{MAC: macA, Config: Config{Mask: ip("255.0.255.0")}}})[0]; it.Status != Invalid {
		t.Errorf("non-contiguous mask accepted: %+v", it)
	}
	it := Build(devs, []Request{{MAC: macA, Config: Config{Gateway: ip("10.0.0.1")}}})[0]
	if it.Status != Planned || len(it.Warnings) == 0 || !strings.Contains(it.Warnings[0], "outside") {
		t.Errorf("gateway outside the subnet should be planned with a warning: %+v", it)
	}
}

func TestBuildAddressConflicts(t *testing.T) {
	devs := fleet(dev(macA, "192.168.1.10"), dev(macB, "192.168.1.11"), dev(macC, "192.168.1.12"))

	// Onto a camera that is not part of the batch.
	items := Build(devs, []Request{{MAC: macA, Config: Config{IP: ip("192.168.1.12")}}})
	if items[0].Status != Invalid || !strings.Contains(items[0].Error, macC) {
		t.Errorf("moving onto an address in use must be invalid and name the holder: %+v", items[0])
	}

	// Two cameras given the same address: both are reported.
	items = Build(devs, []Request{
		{MAC: macA, Config: Config{IP: ip("192.168.1.50")}}, {MAC: macB, Config: Config{IP: ip("192.168.1.50")}}})
	if items[0].Status != Invalid || items[1].Status != Invalid {
		t.Errorf("duplicate targets: %s / %s, want both invalid", items[0].Status, items[1].Status)
	}

	// Onto a camera of the batch that keeps its address.
	items = Build(devs, []Request{
		{MAC: macA, Config: Config{IP: ip("192.168.1.11")}}, {MAC: macB, Config: Config{Gateway: ip("192.168.1.254")}}})
	if items[0].Status != Invalid || items[1].Status != Planned {
		t.Errorf("%s / %s, want invalid / planned", items[0].Status, items[1].Status)
	}

	// A swap and a shift are fine: each address is free by the time the batch is done.
	items = Build(devs, []Request{
		{MAC: macA, Config: Config{IP: ip("192.168.1.11")}}, {MAC: macB, Config: Config{IP: ip("192.168.1.10")}}})
	if Blocked(items) {
		t.Errorf("a swap was rejected: %+v", items)
	}
	items = Build(devs, []Request{
		{MAC: macA, Config: Config{IP: ip("192.168.1.11")}}, {MAC: macB, Config: Config{IP: ip("192.168.1.12")}},
		{MAC: macC, Config: Config{IP: ip("192.168.1.13")}}})
	if Blocked(items) {
		t.Errorf("a shift was rejected: %+v", items)
	}

	// Cameras that already share an address and keep it are not this batch's problem.
	shared := fleet(dev(macA, "192.168.1.88"), dev(macB, "192.168.1.88"))
	gw := Config{Gateway: ip("192.168.1.254")}
	if items = Build(shared, []Request{{MAC: macA, Config: gw}, {MAC: macB, Config: gw}}); Blocked(items) {
		t.Errorf("an in-place change on cameras sharing an address was rejected: %+v", items)
	}
}

func TestSequence(t *testing.T) {
	got, err := Sequence(ip("192.168.1.254"), 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"192.168.1.254", "192.168.2.0", "192.168.2.2"} {
		if got[i].String() != want {
			t.Errorf("address %d: %s, want %s", i, got[i], want)
		}
	}
	if _, err := Sequence(ip("255.255.255.254"), 1, 3); err == nil {
		t.Error("running past 255.255.255.255 must fail")
	}
	if _, err := Sequence(ip("192.168.1.1"), 0, 3); err == nil {
		t.Error("a step of 0 must fail")
	}
}

func TestCheckAddress(t *testing.T) {
	for _, c := range []struct {
		ip, mask string
		ok       bool
	}{
		{"192.168.1.10", "255.255.255.0", true},
		{"192.168.1.0", "255.255.255.0", false},   // network
		{"192.168.1.255", "255.255.255.0", false}, // broadcast
		{"10.1.255.255", "255.0.0.0", true},       // only looks like a broadcast
		{"192.168.1.10", "255.255.0.255", false},  // not a netmask
		{"192.168.1.1", "255.255.255.254", true},  // /31 has no network/broadcast
	} {
		if _, err := CheckAddress(ip(c.ip), ip(c.mask)); (err == nil) != c.ok {
			t.Errorf("CheckAddress(%s, %s): err=%v, want ok=%v", c.ip, c.mask, err, c.ok)
		}
	}
}

// The reason confirmation compares every field: with the address unchanged, only the others can show the change.
func TestConfigMatchesComparesEveryField(t *testing.T) {
	d := dev(macA, "192.168.1.10")
	c, err := ConfigOf(d)
	if err != nil || !c.Matches(d) {
		t.Fatalf("a device must match its own configuration (err %v)", err)
	}
	for name, change := range map[string]func(*Config){
		"ip":      func(c *Config) { c.IP = ip("192.168.1.11") },
		"mask":    func(c *Config) { c.Mask = ip("255.255.0.0") },
		"gateway": func(c *Config) { c.Gateway = ip("192.168.1.254") },
		"dns1":    func(c *Config) { c.DNS1 = ip("1.1.1.1") },
		"dns2":    func(c *Config) { c.DNS2 = ip("1.1.1.1") },
	} {
		other := c
		change(&other)
		if other.Matches(d) {
			t.Errorf("a different %s still matched", name)
		}
	}
	if c.Matches(nil) {
		t.Error("nothing matches a device that was not seen")
	}
}
