package batch

import (
	"fmt"
	"net/netip"
	"sort"

	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

// RescueInput describes the network that stranded devices are to be brought into.
type RescueInput struct {
	Subnet netip.Prefix // the host's subnet on the interface in use
	Host   netip.Addr   // the host's own address in it (never handed out)

	Gateway netip.Addr // zero: copy the gateway the devices already in Subnet use, else assume its first address
	DNS1    netip.Addr // zero: the gateway
	DNS2    netip.Addr // zero: keep each device's current value

	// Taken lists further addresses that must not be handed out — ones found to be in use by something that is
	// not a camera.
	Taken map[netip.Addr]bool
}

// Rescue plans the move that the tool exists for: devices that announce an address outside the host's subnet —
// visible by multicast, unreachable by IP — each get a free address inside it, with the subnet's mask and gateway.
// Devices in macs that are already inside the subnet get an empty request (Build reports them unchanged), and so do
// ones that were not seen (Build reports them not-seen).
//
// Free addresses are taken right after the highest device already in the subnet, so the cameras stay together; with
// no device there yet, from the top of the subnet downwards. An address counts as free when no discovered device
// announces it and it is not the host, the gateway or in Taken — which says nothing about printers and laptops,
// hence Taken.
//
// gatewayAssumed is true when there was nothing to copy the gateway from and the subnet's first address was used.
func Rescue(devices map[string]*mhed.Device, macs []string, in RescueInput) (reqs []Request, gatewayAssumed bool, err error) {
	if !in.Subnet.IsValid() || !in.Subnet.Addr().Is4() || in.Subnet.Bits() > 30 {
		return nil, false, fmt.Errorf("%s is not an IPv4 subnet with room for other hosts", in.Subnet)
	}
	subnet := in.Subnet.Masked()
	first, last := toU32(subnet.Addr())+1, toU32(broadcast(subnet))-1

	// What is already there: every discovered address is off limits, and the devices inside the subnet say where
	// the cameras live and which gateway they use.
	used := map[netip.Addr]bool{in.Host: true}
	for a := range in.Taken {
		used[a] = true
	}
	gateways := map[netip.Addr]int{}
	var highest uint32
	for _, d := range devices {
		a, err := netip.ParseAddr(d.IP)
		if err != nil {
			continue
		}
		used[a] = true
		if !subnet.Contains(a) {
			continue
		}
		if v := toU32(a); v > highest {
			highest = v
		}
		if gw, err := netip.ParseAddr(d.Gateway); err == nil && subnet.Contains(gw) && gw != subnet.Addr() {
			gateways[gw]++
		}
	}

	gw := in.Gateway
	if !gw.IsValid() {
		type vote struct {
			gw netip.Addr
			n  int
		}
		var votes []vote
		for g, n := range gateways {
			votes = append(votes, vote{g, n})
		}
		sort.Slice(votes, func(i, j int) bool { // most used first; ties go to the lower address, deterministically
			if votes[i].n != votes[j].n {
				return votes[i].n > votes[j].n
			}
			return votes[i].gw.Less(votes[j].gw)
		})
		if len(votes) > 0 {
			gw = votes[0].gw
		} else {
			gw, gatewayAssumed = fromU32(first), true
		}
	}
	used[gw] = true
	dns1 := in.DNS1
	if !dns1.IsValid() {
		dns1 = gw
	}
	mask := maskOf(subnet.Bits())

	// Candidate addresses in the order described above.
	next := func() func() (netip.Addr, bool) {
		// Generated lazily, so a /8 costs nothing.
		if highest != 0 {
			v, wrapped := highest+1, false
			return func() (netip.Addr, bool) {
				for {
					if !wrapped && v > last {
						v, wrapped = first, true
					}
					if wrapped && v >= highest {
						return netip.Addr{}, false
					}
					a := fromU32(v)
					v++
					if !used[a] {
						return a, true
					}
				}
			}
		}
		v := last
		return func() (netip.Addr, bool) {
			for v >= first {
				a := fromU32(v)
				v--
				if !used[a] {
					return a, true
				}
			}
			return netip.Addr{}, false
		}
	}()

	reqs = make([]Request, len(macs))
	for i, mac := range macs {
		reqs[i] = Request{MAC: mac}
		d := devices[normalizeMAC(mac)]
		if d == nil {
			continue
		}
		if a, err := netip.ParseAddr(d.IP); err == nil && subnet.Contains(a) {
			continue
		}
		a, ok := next()
		if !ok {
			return nil, false, fmt.Errorf("%s has no free address left for %s (%d of %d placed)", subnet, mac, i, len(macs))
		}
		used[a] = true
		reqs[i].Config = Config{IP: a, Mask: mask, Gateway: gw, DNS1: dns1, DNS2: in.DNS2}
	}
	return reqs, gatewayAssumed, nil
}

func toU32(a netip.Addr) uint32 {
	b := a.As4()
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func fromU32(v uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

func maskOf(bits int) netip.Addr {
	if bits == 0 {
		return fromU32(0)
	}
	return fromU32(^uint32(0) << (32 - bits))
}
