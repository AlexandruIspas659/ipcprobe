// Package fakecam is a software camera that speaks the device side of the MHED protocol. It announces itself on
// 234.55.55.56 at a fixed interval, listens on 234.55.55.55 for set-network commands addressed to its MAC, acks
// every one it receives (like real firmware, regardless of password), and applies the change only when the
// password matches.
//
// It exists so the discovery layer and the CLI can be exercised end to end — in `go test` and by hand with
// cmd/fakecam — without a camera on the desk.
package fakecam

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

// Config describes the simulated device.
type Config struct {
	Device   mhed.Device   // identity and current network config; MAC is required
	Password string        // admin password that set-network must carry
	Interval time.Duration // announce period; 0 means 500 ms
}

// Applied records one accepted set-network command.
type Applied struct {
	IP, Mask, Gateway, DNS1, DNS2 string
	At                            time.Time
}

// Camera is a running fake camera. Stop it with Close.
type Camera struct {
	cfg   Config
	ifIP  netip.Addr
	rx    *net.UDPConn
	mu    sync.Mutex
	dev   mhed.Device
	seen  int // set-network commands received (acked), right or wrong password
	log   []Applied
	done  chan struct{}
	wg    sync.WaitGroup
	debug func(string, ...any)
}

// Start brings up a fake camera on the given interface (which must carry ifIP). Announcements and acks are sent
// from ifIP so that a tool on the same host or segment hears them.
func Start(ifi *net.Interface, ifIP netip.Addr, cfg Config, debug func(string, ...any)) (*Camera, error) {
	if _, err := net.ParseMAC(cfg.Device.MAC); err != nil {
		return nil, fmt.Errorf("fakecam: MAC: %w", err)
	}
	if _, err := mhed.BuildAnnounce(cfg.Device); err != nil {
		return nil, fmt.Errorf("fakecam: device config: %w", err)
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 500 * time.Millisecond
	}
	if debug == nil {
		debug = func(string, ...any) {}
	}
	group := &net.UDPAddr{IP: net.ParseIP(mhed.GroupTX), Port: mhed.Port}
	rx, err := net.ListenMulticastUDP("udp4", ifi, group)
	if err != nil {
		return nil, fmt.Errorf("fakecam: join %s on %s: %w", mhed.GroupTX, ifi.Name, err)
	}
	c := &Camera{cfg: cfg, ifIP: ifIP, rx: rx, dev: cfg.Device, done: make(chan struct{}), debug: debug}
	c.wg.Add(2)
	go c.announceLoop()
	go c.commandLoop()
	return c, nil
}

// Close stops the camera and releases its sockets.
func (c *Camera) Close() {
	select {
	case <-c.done:
		return
	default:
	}
	close(c.done)
	_ = c.rx.Close()
	c.wg.Wait()
}

// Device returns the camera's current (possibly reconfigured) state.
func (c *Camera) Device() mhed.Device {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dev
}

// Received is how many set-network commands addressed to this camera have arrived (all are acked).
func (c *Camera) Received() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen
}

// AppliedLog lists the set-network commands that carried the right password and were applied.
func (c *Camera) AppliedLog() []Applied {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Applied(nil), c.log...)
}

func (c *Camera) send(pkt []byte, group string) {
	local := &net.UDPAddr{IP: net.IP(c.ifIP.AsSlice())}
	dst := &net.UDPAddr{IP: net.ParseIP(group), Port: mhed.Port}
	uc, err := net.DialUDP("udp4", local, dst)
	if err != nil {
		c.debug("fakecam: dial: %v", err)
		return
	}
	defer uc.Close()
	if _, err := uc.Write(pkt); err != nil {
		c.debug("fakecam: send: %v", err)
	}
}

func (c *Camera) announceLoop() {
	defer c.wg.Done()
	t := time.NewTicker(c.cfg.Interval)
	defer t.Stop()
	for {
		pkt, err := mhed.BuildAnnounce(c.Device())
		if err == nil {
			c.send(pkt, mhed.GroupRX)
		}
		select {
		case <-c.done:
			return
		case <-t.C:
		}
	}
}

func (c *Camera) commandLoop() {
	defer c.wg.Done()
	myMAC, _ := net.ParseMAC(c.cfg.Device.MAC)
	buf := make([]byte, 2048)
	for {
		n, src, err := c.rx.ReadFromUDP(buf)
		if err != nil {
			return // closed
		}
		pkt := buf[:n]
		cmd, ok := mhed.Command(pkt)
		if !ok || cmd != mhed.CmdSetNet || len(pkt) < 140 {
			continue // searches need no reply: we announce on a timer anyway, like real firmware
		}
		if !equalBytes(pkt[32:38], myMAC) {
			continue
		}
		reqIP, _ := netip.AddrFromSlice(src.IP.To4())
		ack, err := mhed.BuildSetAck(reqIP, uint16(src.Port))
		if err == nil {
			c.send(ack, mhed.GroupRX)
		}
		pw := string(trimZero(pkt[84:112]))
		want := base64.StdEncoding.EncodeToString([]byte(c.cfg.Password))
		c.mu.Lock()
		c.seen++
		if pw == want {
			c.dev.IP = ip4(pkt, 40)
			c.dev.Mask = ip4(pkt, 44)
			c.dev.Gateway = ip4(pkt, 48)
			c.dev.DNS1 = ip4(pkt, 112)
			c.dev.DNS2 = ip4(pkt, 116)
			c.log = append(c.log, Applied{c.dev.IP, c.dev.Mask, c.dev.Gateway, c.dev.DNS1, c.dev.DNS2, time.Now()})
			c.debug("fakecam: applied %s/%s gw %s from %s", c.dev.IP, c.dev.Mask, c.dev.Gateway, src)
		} else {
			c.debug("fakecam: wrong password from %s — acked, not applied", src)
		}
		c.mu.Unlock()
	}
}

func ip4(pkt []byte, off int) string {
	a, _ := netip.AddrFromSlice(pkt[off : off+4])
	return a.String()
}

func trimZero(b []byte) []byte {
	for i, c := range b {
		if c == 0 {
			return b[:i]
		}
	}
	return b
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
