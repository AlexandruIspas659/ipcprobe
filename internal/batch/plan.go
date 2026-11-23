// Package batch plans and carries out a network change on several devices at once.
//
// Planning (this file) is pure: given what discovery saw and what the caller asked for, it works out each device's
// target configuration, validates it, and refuses plans that would put two devices on one address. Carrying the
// plan out (run.go) is the only part that touches the network.
//
// A request names a device by MAC and gives any subset of IP / mask / gateway / DNS1 / DNS2; whatever it leaves out
// is kept from the device's current announcement. That one rule covers both ways a batch is used: handing a row of
// new cameras consecutive addresses (every request carries an IP) and changing the gateway or DNS of a fleet in
// place (no request carries an IP).
package batch

import (
	"fmt"
	"net"
	"net/netip"
	"sort"

	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

// Config is one device's network configuration — exactly the fields a set-network packet carries.
type Config struct {
	IP      netip.Addr `json:"ip"`
	Mask    netip.Addr `json:"mask"`
	Gateway netip.Addr `json:"gateway"`
	DNS1    netip.Addr `json:"dns1"`
	DNS2    netip.Addr `json:"dns2"`
}

// ConfigOf reads the configuration a device currently announces.
func ConfigOf(d *mhed.Device) (Config, error) {
	var c Config
	for _, f := range []struct {
		dst  *netip.Addr
		name string
		s    string
	}{{&c.IP, "ip", d.IP}, {&c.Mask, "mask", d.Mask}, {&c.Gateway, "gateway", d.Gateway}, {&c.DNS1, "dns1", d.DNS1}, {&c.DNS2, "dns2", d.DNS2}} {
		a, err := netip.ParseAddr(f.s)
		if err != nil || !a.Is4() {
			return Config{}, fmt.Errorf("announces an invalid %s %q", f.name, f.s)
		}
		*f.dst = a
	}
	return c, nil
}

// Matches reports whether d announces exactly this configuration. Every field is compared, not just the IP: a
// change that keeps the address (a new gateway, say) is only visible in the other fields, and a device that
// ignored the command because the password was wrong still announces at the requested IP.
func (c Config) Matches(d *mhed.Device) bool {
	return d != nil && d.IP == c.IP.String() && d.Mask == c.Mask.String() && d.Gateway == c.Gateway.String() &&
		d.DNS1 == c.DNS1.String() && d.DNS2 == c.DNS2.String()
}

// CheckAddress verifies that mask is a contiguous netmask and that ip is a usable host address in the subnet the two
// define (not its network or broadcast address). It returns that subnet.
func CheckAddress(ip, mask netip.Addr) (netip.Prefix, error) {
	if !ip.Is4() || !mask.Is4() {
		return netip.Prefix{}, fmt.Errorf("%s / %s is not an IPv4 address and netmask", ip, mask)
	}
	m := mask.As4()
	bits, size := net.IPv4Mask(m[0], m[1], m[2], m[3]).Size() // size == 0 means non-contiguous
	if size == 0 {
		return netip.Prefix{}, fmt.Errorf("%s is not a valid netmask", mask)
	}
	prefix := netip.PrefixFrom(ip, bits)
	if bits < 31 {
		subnet := prefix.Masked()
		if ip == subnet.Addr() || ip == broadcast(subnet) {
			return netip.Prefix{}, fmt.Errorf("%s is the network or broadcast address of %s", ip, subnet)
		}
	}
	return prefix, nil
}

func broadcast(p netip.Prefix) netip.Addr {
	a := p.Masked().Addr().As4()
	for i := 0; i < 32-p.Bits(); i++ {
		a[3-i/8] |= 1 << (uint(i) % 8)
	}
	return netip.AddrFrom4(a)
}

// Sequence returns n IPv4 addresses: start, start+step, start+2·step, …
func Sequence(start netip.Addr, step, n int) ([]netip.Addr, error) {
	if !start.Is4() {
		return nil, fmt.Errorf("start address must be IPv4: %v", start)
	}
	if step < 1 {
		return nil, fmt.Errorf("step must be at least 1, got %d", step)
	}
	b := start.As4()
	v := uint64(b[0])<<24 | uint64(b[1])<<16 | uint64(b[2])<<8 | uint64(b[3])
	out := make([]netip.Addr, 0, n)
	for i := 0; i < n; i++ {
		x := v + uint64(i)*uint64(step)
		if x > 0xFFFFFFFF {
			return nil, fmt.Errorf("address range runs past 255.255.255.255 after %d device(s)", i)
		}
		out = append(out, netip.AddrFrom4([4]byte{byte(x >> 24), byte(x >> 16), byte(x >> 8), byte(x)}))
	}
	return out, nil
}

// Request is what the caller wants for one device. Fields of Config left as the zero netip.Addr mean "keep what
// the device has now".
type Request struct {
	MAC string
	Config
}

// Status is where one device stands, before or after a run.
type Status string

const (
	Planned      Status = "planned"       // will be sent a set-network command
	Unchanged    Status = "unchanged"     // already has the target configuration; nothing is sent
	NotSeen      Status = "not-seen"      // not announcing, so there is no current configuration to build on
	Invalid      Status = "invalid"       // the request cannot be applied (see Error); blocks the whole batch
	Confirmed    Status = "confirmed"     // re-announced with the target configuration
	NotConfirmed Status = "not-confirmed" // sent, but did not re-announce with the target configuration in time
	NotSent      Status = "not-sent"      // skipped: the test device failed, or the datagram could not be sent
)

// Item is one device in a batch: what it has, what it should get, and how that went.
type Item struct {
	MAC      string   `json:"mac"`
	Name     string   `json:"name"`
	Model    string   `json:"model"`
	Current  *Config  `json:"current"` // nil when the device was not seen
	Target   *Config  `json:"target"`  // nil when it could not be worked out
	Changes  []string `json:"changes"` // the fields that differ between Current and Target
	Status   Status   `json:"status"`
	Acked    bool     `json:"acked"` // the device acknowledged receipt (which says nothing about the password)
	Warnings []string `json:"warnings"`
	Error    string   `json:"error,omitempty"`
}

func (it *Item) fail(msg string) {
	it.Status = Invalid
	it.Error = msg
}

func (it *Item) changes(field string) bool {
	for _, c := range it.Changes {
		if c == field {
			return true
		}
	}
	return false
}

// Build resolves reqs against the devices discovery saw. The result has one Item per request, in the same order.
// Nothing should be sent if any item is Invalid — see Blocked.
func Build(devices map[string]*mhed.Device, reqs []Request) []Item {
	items := make([]Item, len(reqs))
	byMAC := map[string]*Item{}
	for i, r := range reqs {
		it := &items[i]
		it.MAC, it.Changes, it.Warnings = r.MAC, []string{}, []string{}
		hw, err := net.ParseMAC(r.MAC)
		if err != nil || len(hw) != 6 {
			it.fail("invalid MAC address")
			continue
		}
		it.MAC = hw.String()
		if byMAC[it.MAC] != nil {
			it.fail("listed more than once")
			continue
		}
		byMAC[it.MAC] = it
		dev := devices[it.MAC]
		if dev == nil {
			it.Status = NotSeen
			it.Error = "not seen in discovery"
			continue
		}
		it.Name, it.Model = dev.Name, dev.Model
		cur, err := ConfigOf(dev)
		if err != nil {
			it.fail(err.Error())
			continue
		}
		it.Current = &cur
		tgt := cur
		for _, f := range []struct {
			dst *netip.Addr
			req netip.Addr
		}{{&tgt.IP, r.IP}, {&tgt.Mask, r.Mask}, {&tgt.Gateway, r.Gateway}, {&tgt.DNS1, r.DNS1}, {&tgt.DNS2, r.DNS2}} {
			if f.req.IsValid() {
				*f.dst = f.req
			}
		}
		it.Target = &tgt
		for _, a := range []netip.Addr{tgt.IP, tgt.Mask, tgt.Gateway, tgt.DNS1, tgt.DNS2} {
			if !a.Is4() {
				it.fail(fmt.Sprintf("%s is not an IPv4 address", a))
			}
		}
		if it.Status == Invalid {
			continue
		}
		prefix, err := CheckAddress(tgt.IP, tgt.Mask)
		if err != nil {
			it.fail(err.Error())
			continue
		}
		for _, f := range []struct {
			name     string
			old, new netip.Addr
		}{{"ip", cur.IP, tgt.IP}, {"mask", cur.Mask, tgt.Mask}, {"gateway", cur.Gateway, tgt.Gateway}, {"dns1", cur.DNS1, tgt.DNS1}, {"dns2", cur.DNS2, tgt.DNS2}} {
			if f.old != f.new {
				it.Changes = append(it.Changes, f.name)
			}
		}
		if len(it.Changes) == 0 {
			it.Status = Unchanged
			continue
		}
		if tgt.IP == tgt.Gateway && (it.changes("ip") || it.changes("gateway")) {
			it.fail(fmt.Sprintf("%s would be both the camera's address and its gateway", tgt.IP))
			continue
		}
		it.Status = Planned
		if !tgt.Gateway.IsUnspecified() && !prefix.Contains(tgt.Gateway) {
			it.Warnings = append(it.Warnings, fmt.Sprintf("gateway %s is outside %s", tgt.Gateway, prefix.Masked()))
		}
		if it.changes("gateway") && !it.changes("dns1") && cur.DNS1 == cur.Gateway && !cur.Gateway.IsUnspecified() {
			it.Warnings = append(it.Warnings, fmt.Sprintf("DNS 1 stays %s, the old gateway", cur.DNS1))
		}
	}

	// Address conflicts. Where every discovered device will end up: its target if it is in the batch, otherwise
	// where it is now. A device that changes address must not land on any other device's final address. Devices
	// that keep their address are never flagged — two factory-fresh cameras sharing an IP are not this batch's doing.
	final := map[string]netip.Addr{}
	for mac, dev := range devices {
		if it := byMAC[mac]; it != nil && it.Target != nil && (it.Status == Planned || it.Status == Unchanged) {
			final[mac] = it.Target.IP
		} else if a, err := netip.ParseAddr(dev.IP); err == nil {
			final[mac] = a
		}
	}
	macs := make([]string, 0, len(final))
	for mac := range final {
		macs = append(macs, mac)
	}
	sort.Strings(macs)
	var conflicts []func()
	for i := range items {
		it := &items[i]
		if it.Status != Planned || !it.changes("ip") {
			continue
		}
		for _, mac := range macs {
			if mac == it.MAC || final[mac] != it.Target.IP {
				continue
			}
			msg := fmt.Sprintf("%s is in use by %s", it.Target.IP, mac)
			if other := byMAC[mac]; other != nil && other.Status == Planned && other.changes("ip") {
				msg = fmt.Sprintf("%s is also assigned to %s in this batch", it.Target.IP, mac)
			}
			conflicts = append(conflicts, func() { it.fail(msg) })
			break
		}
	}
	for _, mark := range conflicts { // marked afterwards, so both sides of a clash are reported
		mark()
	}
	return items
}

// normalizeMAC returns mac in the form devices are keyed by, or mac itself if it does not parse.
func normalizeMAC(mac string) string {
	if hw, err := net.ParseMAC(mac); err == nil && len(hw) == 6 {
		return hw.String()
	}
	return mac
}

// Blocked reports whether any item is Invalid, in which case nothing in the batch should be sent.
func Blocked(items []Item) bool {
	for i := range items {
		if items[i].Status == Invalid {
			return true
		}
	}
	return false
}

// Count returns how many items have status s.
func Count(items []Item, s Status) int {
	n := 0
	for i := range items {
		if items[i].Status == s {
			n++
		}
	}
	return n
}
