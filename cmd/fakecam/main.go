// Command fakecam runs a software MHED camera on one interface so ipcprobe (or the GUI) can be exercised without
// hardware. It is a development tool and is not part of releases.
//
//	fakecam [--iface en0] [--mac 02:00:00:00:00:01] [--ip 192.168.1.69] [--name lobby] [--password admin]
//
// Then, in another terminal:  ipcprobe list   /   ipcprobe set --mac 02:00:00:00:00:01 --ip ... --password admin
package main

import (
	"flag"
	"fmt"
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
	flag.Parse()

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
	cfg := fakecam.Config{
		Device: mhed.Device{
			MAC: *mac, IP: *ip, Mask: *mask, Gateway: *gw, DNS1: *gw, DNS2: "8.8.8.8",
			Name: *name, Serial: "FAKESERIAL01", Firmware: "1.5-0000000", Model: *model, Vendor: "FAKE",
			HTTP: 80, RTSP: 554, Build: "2026-01-01",
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
	fmt.Fprintf(os.Stderr, "fakecam %s announcing as %s at %s on %s (password %q) — Ctrl-C to stop\n",
		*mac, *name, *ip, sel, *pw)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	d := cam.Device()
	fmt.Fprintf(os.Stderr, "\nfakecam: %d set-network command(s) received, %d applied; final address %s/%s gw %s\n",
		cam.Received(), len(cam.AppliedLog()), d.IP, d.Mask, d.Gateway)
}
