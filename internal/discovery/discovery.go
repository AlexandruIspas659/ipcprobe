// Package discovery is the I/O layer for ipcprobe: interface selection, multicast sockets, and the discover loop.
//
// It uses only the Go standard library. Multicast group membership is handled with net.ListenMulticastUDP (one
// receive socket per interface); sends select their egress interface by binding the socket's local address to the
// interface IP. That keeps the package portable across macOS/Linux/Windows with no external modules.
package discovery

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

// Iface is a usable IPv4 interface.
type Iface struct {
	Name  string
	IP    netip.Addr
	Net   netip.Prefix
	iface *net.Interface
}

func (i Iface) String() string { return fmt.Sprintf("%s (%s)", i.Name, i.IP) }

// Interfaces returns every up, non-loopback interface that has an IPv4 address.
func Interfaces() ([]Iface, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []Iface
	for i := range ifs {
		in := ifs[i]
		if in.Flags&net.FlagUp == 0 || in.Flags&net.FlagLoopback != 0 || in.Flags&net.FlagMulticast == 0 {
			continue
		}
		addrs, err := in.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := ipnet.IP.To4()
			if v4 == nil {
				continue
			}
			ip, _ := netip.AddrFromSlice(v4)
			ones, _ := ipnet.Mask.Size()
			out = append(out, Iface{Name: in.Name, IP: ip, Net: netip.PrefixFrom(ip, ones).Masked(), iface: &ifs[i]})
		}
	}
	return out, nil
}

// Choose resolves which interfaces to use. ifaceArg restricts to one interface by name or IPv4 address; otherwise,
// if any prefer address falls inside an interface's subnet, those interfaces are used; else all of them.
func Choose(ifaceArg string, prefer ...netip.Addr) ([]Iface, error) {
	all, err := Interfaces()
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no usable IPv4 interface found")
	}
	if ifaceArg != "" {
		if want, err := netip.ParseAddr(ifaceArg); err == nil {
			for _, i := range all {
				if i.IP == want {
					return []Iface{i}, nil
				}
			}
			return nil, fmt.Errorf("no interface has address %s", want)
		}
		var sel []Iface
		for _, i := range all {
			if i.Name == ifaceArg {
				sel = append(sel, i)
			}
		}
		if len(sel) == 0 {
			var names []string
			for _, i := range all {
				names = append(names, i.Name)
			}
			return nil, fmt.Errorf("interface %q has no IPv4 address (have: %s)", ifaceArg, strings.Join(names, ", "))
		}
		return sel, nil
	}
	for _, p := range prefer {
		var sel []Iface
		for _, i := range all {
			if i.Net.Contains(p) {
				sel = append(sel, i)
			}
		}
		if len(sel) > 0 {
			return sel, nil
		}
	}
	return all, nil
}

// Ack is a set-network acknowledgement from a device.
type Ack struct {
	From      string // the device's source IP
	Requester string // echoed tool IP
	Port      uint16 // echoed tool source port
}

// Conn is a set of joined multicast receive sockets plus the interfaces to send from.
type Conn struct {
	rx     []*net.UDPConn
	ifaces []Iface
}

// Open binds a receive socket joined to GroupRX on each interface. Interfaces that can't join the group (some
// virtual/bridge interfaces refuse) are skipped with a warning; it only fails if none could join.
func Open(ifaces []Iface) (*Conn, error) {
	group := &net.UDPAddr{IP: net.ParseIP(mhed.GroupRX), Port: mhed.Port}
	c := &Conn{}
	for _, ifc := range ifaces {
		pc, err := net.ListenMulticastUDP("udp4", ifc.iface, group)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: cannot join %s on %s (%s): %v\n", mhed.GroupRX, ifc.Name, ifc.IP, err)
			continue
		}
		_ = pc.SetReadBuffer(1 << 20)
		c.rx = append(c.rx, pc)
		c.ifaces = append(c.ifaces, ifc)
	}
	if len(c.rx) == 0 {
		return nil, fmt.Errorf("could not join %s on any of the %d selected interface(s)", mhed.GroupRX, len(ifaces))
	}
	return c, nil
}

// Close releases all sockets.
func (c *Conn) Close() {
	for _, pc := range c.rx {
		_ = pc.Close()
	}
	c.rx = nil
}

func (c *Conn) send(payload []byte) error {
	dst := &net.UDPAddr{IP: net.ParseIP(mhed.GroupTX), Port: mhed.Port}
	var firstErr error
	for _, ifc := range c.ifaces {
		local := &net.UDPAddr{IP: net.IP(ifc.IP.AsSlice())}
		uc, err := net.DialUDP("udp4", local, dst) // binding LocalAddr selects the egress interface
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		_, err = uc.Write(payload)
		_ = uc.Close()
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Probe sends a cmd-1 search out every interface.
func (c *Conn) Probe() error { return c.send(mhed.BuildSearch()) }

// SendSet sends a prebuilt cmd-3 set-network packet out every interface.
func (c *Conn) SendSet(pkt []byte) error { return c.send(pkt) }

// Collect receives for the given duration, returning devices de-duplicated by MAC (newest wins) and any acks seen.
func (c *Conn) Collect(d time.Duration) (map[string]*mhed.Device, []Ack) {
	devices := map[string]*mhed.Device{}
	var acks []Ack
	var mu sync.Mutex
	deadline := time.Now().Add(d)

	var wg sync.WaitGroup
	for _, pc := range c.rx {
		wg.Add(1)
		go func(pc *net.UDPConn) {
			defer wg.Done()
			buf := make([]byte, 2048)
			for {
				if err := pc.SetReadDeadline(deadline); err != nil {
					return
				}
				n, src, err := pc.ReadFromUDP(buf)
				if err != nil {
					return // timeout or closed
				}
				pkt := buf[:n]
				if dev, ok := mhed.ParseAnnounce(pkt); ok {
					dev.Src = src.IP.String()
					mu.Lock()
					devices[dev.MAC] = dev
					mu.Unlock()
				} else if reqIP, reqPort, ok := mhed.ParseSetAck(pkt); ok {
					mu.Lock()
					acks = append(acks, Ack{From: src.IP.String(), Requester: reqIP, Port: reqPort})
					mu.Unlock()
				}
			}
		}(pc)
	}
	wg.Wait()
	return devices, acks
}

// Discover probes then collects for d. Convenience wrapper.
func (c *Conn) Discover(d time.Duration) (map[string]*mhed.Device, []Ack, error) {
	if err := c.Probe(); err != nil {
		return nil, nil, err
	}
	devs, acks := c.Collect(d)
	return devs, acks, nil
}

// SortedByIP returns devices ordered by IPv4 address.
func SortedByIP(m map[string]*mhed.Device) []*mhed.Device {
	out := make([]*mhed.Device, 0, len(m))
	for _, d := range m {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := netip.ParseAddr(out[i].IP)
		b, _ := netip.ParseAddr(out[j].IP)
		return a.Less(b)
	})
	return out
}

// SortedByMAC returns devices ordered by MAC (stable output for --json).
func SortedByMAC(m map[string]*mhed.Device) []*mhed.Device {
	out := make([]*mhed.Device, 0, len(m))
	for _, d := range m {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MAC < out[j].MAC })
	return out
}
