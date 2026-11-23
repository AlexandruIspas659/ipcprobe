// Package neighbor answers one question without any privileges: is something on this network segment already
// using a given IPv4 address?
//
// Discovery only sees cameras. A printer, a laptop or the recorder sitting on the address a camera is about to be
// given is invisible to it, and an address conflict is a miserable thing to debug on site. So before a camera is
// moved, the tool asks the operating system: it sends a throwaway datagram to the address, which makes the kernel
// resolve it (ARP), and then reads the kernel's neighbour table. An address that resolved has an owner.
//
// This needs no raw sockets and no root, uses only the standard library, and works on anything that is on-link —
// which is also its limit: an address in a subnet this computer is not on cannot be checked this way.
package neighbor

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Owner is what is known about the holder of an address.
type Owner struct {
	MAC net.HardwareAddr
	// Fresh is true when the holder showed itself during the check (it answered, or its entry appeared in the
	// neighbour table only now). False means the entry was already cached: the holder was there within the last
	// few minutes, and may have left since.
	Fresh bool
}

// Table is a snapshot of the system's neighbour (ARP) table: the addresses that currently resolve to a hardware
// address.
type Table map[netip.Addr]net.HardwareAddr

// ErrUnsupported is returned where the package does not know how to read the neighbour table.
var ErrUnsupported = errors.New("reading the neighbour table is not supported on this system")

// InUse reports which of addrs are held by something on the local segment. Every address must be on-link (inside
// a subnet of one of this computer's interfaces) — the caller checks that; an address that is not would be probed
// through the default gateway and never resolve. wait is how long holders are given to answer; about a second is
// plenty on a LAN.
func InUse(addrs []netip.Addr, wait time.Duration) (map[netip.Addr]Owner, error) {
	if len(addrs) == 0 {
		return map[netip.Addr]Owner{}, nil
	}
	before, err := Read()
	if err != nil {
		return nil, err
	}
	answered := nudge(addrs, wait)
	after, err := Read()
	if err != nil {
		return nil, err
	}
	out := map[netip.Addr]Owner{}
	for _, a := range addrs {
		mac, ok := after[a]
		if !ok {
			continue
		}
		_, cached := before[a]
		out[a] = Owner{MAC: mac, Fresh: !cached || answered[a]}
	}
	return out, nil
}

// nudge sends a datagram to the discard port of every address, which is what makes the kernel resolve it. As a
// bonus, a host that is up and not firewalled answers "port unreachable", which the kernel reports on the connected
// socket as a refused connection: proof that the holder is there right now, even if its entry was already cached.
func nudge(addrs []netip.Addr, wait time.Duration) map[netip.Addr]bool {
	answered := map[netip.Addr]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	deadline := time.Now().Add(wait)
	for _, a := range addrs {
		wg.Add(1)
		go func(a netip.Addr) {
			defer wg.Done()
			c, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(netip.AddrPortFrom(a, 9)))
			if err != nil {
				time.Sleep(time.Until(deadline))
				return
			}
			defer c.Close()
			_ = c.SetDeadline(deadline)
			buf := make([]byte, 16)
			for i := 0; i < 2 && time.Now().Before(deadline); i++ { // twice: the first datagram can be lost to ARP itself
				if _, err := c.Write([]byte{0}); errors.Is(err, syscall.ECONNREFUSED) {
					mu.Lock()
					answered[a] = true
					mu.Unlock()
					break
				}
				_ = c.SetReadDeadline(minTime(deadline, time.Now().Add(wait/2)))
				if _, err := c.Read(buf); errors.Is(err, syscall.ECONNREFUSED) {
					mu.Lock()
					answered[a] = true
					mu.Unlock()
					break
				}
			}
			time.Sleep(time.Until(deadline)) // everyone gets the full wait, so the table is read once it has settled
		}(a)
	}
	wg.Wait()
	return answered
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Read returns the system's neighbour table.
func Read() (Table, error) {
	switch runtime.GOOS {
	case "linux":
		data, err := os.ReadFile("/proc/net/arp")
		if err != nil {
			return nil, err
		}
		return parseProcNetARP(data), nil
	case "darwin", "freebsd", "netbsd", "openbsd", "dragonfly":
		out, err := exec.Command("arp", "-an").Output()
		if err != nil {
			return nil, fmt.Errorf("arp -an: %w", err)
		}
		return parseBSD(out), nil
	case "windows":
		out, err := exec.Command("arp", "-a").Output()
		if err != nil {
			return nil, fmt.Errorf("arp -a: %w", err)
		}
		return parseWindows(out), nil
	}
	return nil, ErrUnsupported
}

// parseBSD reads `arp -an` as macOS and the BSDs print it:
//
//	? (192.168.1.1) at 0:1b:21:4a:b5:f1 on en0 ifscope [ethernet]
//	? (192.168.1.111) at (incomplete) on en0 ifscope [ethernet]
func parseBSD(out []byte) Table {
	t := Table{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || f[2] != "at" || !strings.HasPrefix(f[1], "(") || !strings.HasSuffix(f[1], ")") {
			continue
		}
		add(t, f[1][1:len(f[1])-1], f[3], ":")
	}
	return t
}

// parseProcNetARP reads Linux's /proc/net/arp:
//
//	IP address       HW type     Flags       HW address            Mask     Device
//	192.168.1.1      0x1         0x2         00:1b:21:4a:b5:f1     *        eth0
//
// Flags bit 0x2 is "complete"; an unresolved entry has flags 0x0 and an all-zero address.
func parseProcNetARP(data []byte) Table {
	t := Table{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 {
			continue
		}
		flags, err := strconv.ParseUint(strings.TrimPrefix(f[2], "0x"), 16, 32)
		if err != nil || flags&0x2 == 0 {
			continue
		}
		add(t, f[0], f[3], ":")
	}
	return t
}

// parseWindows reads `arp -a` as Windows prints it:
//
//	Interface: 192.168.1.50 --- 0x4
//	  Internet Address      Physical Address      Type
//	  192.168.1.1           00-1b-21-4a-b5-f1     dynamic
func parseWindows(out []byte) Table {
	t := Table{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 {
			continue
		}
		add(t, f[0], f[1], "-")
	}
	return t
}

// add records ip -> mac if both parse and the hardware address belongs to one host (not broadcast, not multicast,
// not all zeros).
func add(t Table, ip, mac, sep string) {
	a, err := netip.ParseAddr(ip)
	if err != nil || !a.Is4() {
		return
	}
	hw := parseMAC(mac, sep)
	if hw == nil || hw[0]&1 != 0 { // the low bit of the first byte marks group addresses, broadcast included
		return
	}
	zero := true
	for _, b := range hw {
		if b != 0 {
			zero = false
		}
	}
	if zero {
		return
	}
	t[a] = hw
}

// parseMAC accepts six hex groups of one or two digits — macOS prints 0:1b:21:… without leading zeros, which
// net.ParseMAC rejects.
func parseMAC(s, sep string) net.HardwareAddr {
	parts := strings.Split(s, sep)
	if len(parts) != 6 {
		return nil
	}
	hw := make(net.HardwareAddr, 6)
	for i, p := range parts {
		if len(p) < 1 || len(p) > 2 {
			return nil
		}
		v, err := strconv.ParseUint(p, 16, 8)
		if err != nil {
			return nil
		}
		hw[i] = byte(v)
	}
	return hw
}
