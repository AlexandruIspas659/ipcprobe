// Command fakecam runs a software MHED camera on one interface so ipcprobe (or the GUI) can be exercised without
// hardware. It is a development tool and is not part of releases.
//
//	fakecam [--iface en0] [--mac 02:00:00:00:00:01] [--ip 192.168.1.69] [--name lobby] [--password admin]
//	        [--count 5] [--same-ip]
//
// Then, in another terminal:  ipcprobe list   /   ipcprobe set --mac 02:00:00:00:00:01 --ip ... --password admin
//
// --count N runs N cameras, for trying `ipcprobe apply`: the MAC and the IP each count up from --mac and --ip, and
// the name gets a -1, -2, ... suffix. With --same-ip they all sit on --ip, like cameras fresh out of the box.
package main

import (
	"flag"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AlexandruIspas659/ipcprobe/internal/discovery"
	"github.com/AlexandruIspas659/ipcprobe/internal/fakecam"
	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

func main() {
	iface := flag.String("iface", "", "interface name or IPv4 to run on (default: first usable)")
	mac := flag.String("mac", "02:00:00:00:00:01", "camera MAC")
	ip := flag.String("ip", "192.168.1.69", "camera IP")
	mask := flag.String("mask", "255.255.255.0", "netmask")
	gw := flag.String("gw", "192.168.1.1", "gateway")
	name := flag.String("name", "fakecam", "device name")
	model := flag.String("model", "DCN-FAKE1", "model string")
	pw := flag.String("password", "admin", "admin password the camera expects")
	interval := flag.Duration("interval", 500*time.Millisecond, "announce interval")
	count := flag.Int("count", 1, "how many cameras to run (MAC and IP count up from --mac and --ip)")
	sameIP := flag.Bool("same-ip", false, "with --count: give every camera the same --ip, like factory-fresh cameras")
	flag.Parse()

	baseMAC, err := net.ParseMAC(*mac)
	if err != nil || len(baseMAC) != 6 {
		fmt.Fprintf(os.Stderr, "error: invalid --mac %q\n", *mac)
		os.Exit(2)
	}
	baseIP, err := netip.ParseAddr(*ip)
	if err != nil || !baseIP.Is4() || *count < 1 {
		fmt.Fprintln(os.Stderr, "error: --ip must be an IPv4 address and --count at least 1")
		os.Exit(2)
	}

	ifaces, err := discovery.Choose(*iface)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	sel := ifaces[0]
	ifi, err := discovery.NetInterface(sel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	var cams []*fakecam.Camera
	camMAC, camIP := baseMAC, baseIP
	for n := 1; n <= *count; n++ {
		camName := *name
		if *count > 1 {
			camName = fmt.Sprintf("%s-%d", *name, n)
		}
		cfg := fakecam.Config{
			Device: mhed.Device{
				MAC: camMAC.String(), IP: camIP.String(), Mask: *mask, Gateway: *gw, DNS1: *gw, DNS2: "8.8.8.8",
				Name: camName, Serial: fmt.Sprintf("FAKESERIAL%02d", n), Firmware: "1.5-0000000", Model: *model,
				Vendor: "FAKE", HTTP: 80, RTSP: 554, Build: "2026-01-01",
			},
			Password: *pw,
			Interval: *interval,
		}
		cam, err := fakecam.Start(ifi, sel.IP, cfg, func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) })
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		defer cam.Close()
		cams = append(cams, cam)
		fmt.Fprintf(os.Stderr, "fakecam %s announcing as %s at %s on %s (password %q)\n", camMAC, camName, camIP, sel, *pw)

		next := append(net.HardwareAddr(nil), camMAC...) // count the MAC up, carrying into the higher bytes
		for b := 5; b >= 0; b-- {
			if next[b]++; next[b] != 0 {
				break
			}
		}
		camMAC = next
		if !*sameIP {
			camIP = camIP.Next()
		}
	}
	fmt.Fprintln(os.Stderr, "Ctrl-C to stop")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	fmt.Fprintln(os.Stderr)
	for _, cam := range cams {
		d := cam.Device()
		fmt.Fprintf(os.Stderr, "fakecam %s: %d set-network command(s) received, %d applied; final address %s/%s gw %s\n",
			d.MAC, cam.Received(), len(cam.AppliedLog()), d.IP, d.Mask, d.Gateway)
	}
}
