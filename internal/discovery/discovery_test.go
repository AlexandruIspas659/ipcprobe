package discovery

import (
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/AlexandruIspas659/ipcprobe/internal/fakecam"
	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

// End-to-end over real sockets: a fake camera on one interface, the discovery layer on the same interface.
// Exercises multicast join, egress selection, the receive loop, and the set/ack/re-announce sequence — everything
// the unit tests can't. Needs a multicast-capable interface; skipped where there is none (some sandboxes).
func TestEndToEndWithFakeCamera(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	all, err := Interfaces()
	if err != nil || len(all) == 0 {
		t.Skip("no multicast-capable IPv4 interface")
	}
	ifc := all[0]
	ifi, err := NetInterface(ifc)
	if err != nil {
		t.Fatal(err)
	}

	const mac = "02:00:00:00:00:01"
	cam, err := fakecam.Start(ifi, ifc.IP, fakecam.Config{
		Device: mhed.Device{
			MAC: mac, IP: "192.168.77.10", Mask: "255.255.255.0", Gateway: "192.168.77.1",
			DNS1: "192.168.77.1", DNS2: "8.8.8.8", Name: "e2e", Serial: "E2E000000001",
			Firmware: "1.5-1", Model: "DCN-TEST", Vendor: "TEST", HTTP: 80, RTSP: 554, Build: "2026-01-02",
		},
		Password: "s3cret",
		Interval: 200 * time.Millisecond,
	}, t.Logf)
	if err != nil {
		t.Skipf("fake camera could not start on %s: %v", ifc, err)
	}
	defer cam.Close()

	conn, err := Open([]Iface{ifc})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// 1. discovery sees the camera with every field intact
	devs, _, err := conn.Discover(1500 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	d := devs[mac]
	if d == nil {
		t.Skipf("no announcement received on %s — multicast loopback not available here", ifc)
	}
	if d.IP != "192.168.77.10" || d.Name != "e2e" || d.Model != "DCN-TEST" || d.HTTP != 80 || d.Build != "2026-01-02" {
		t.Fatalf("unexpected device: %+v", d)
	}

	hw, _ := net.ParseMAC(mac)
	build := func(ip, pw string) []byte {
		pkt, err := mhed.BuildSetNetwork(hw, netip.MustParseAddr(ip), netip.MustParseAddr("255.255.255.0"),
			netip.MustParseAddr("192.168.77.1"), netip.MustParseAddr("192.168.77.1"), netip.MustParseAddr("8.8.8.8"), pw)
		if err != nil {
			t.Fatal(err)
		}
		return pkt
	}

	// 2. wrong password: the camera acks (receipt) but does not move
	if err := conn.SendSet(build("192.168.77.11", "wrong")); err != nil {
		t.Fatal(err)
	}
	devs, acks := conn.Collect(1 * time.Second)
	if len(acks) == 0 {
		t.Fatal("no ack after set-network with wrong password")
	}
	if cur := devs[mac]; cur != nil && cur.IP != "192.168.77.10" {
		t.Fatalf("camera moved on a wrong password: %+v", cur)
	}
	if cam.Received() != 1 || len(cam.AppliedLog()) != 0 {
		t.Fatalf("received=%d applied=%d, want 1/0", cam.Received(), len(cam.AppliedLog()))
	}

	// 3. right password: ack, then re-announce at the new address
	if err := conn.SendSet(build("192.168.77.11", "s3cret")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	acked, moved := false, false
	for time.Now().Before(deadline) && !moved {
		devs, acks = conn.Collect(500 * time.Millisecond)
		if len(acks) > 0 {
			acked = true
		}
		if cur := devs[mac]; cur != nil && cur.IP == "192.168.77.11" {
			moved = true
		}
	}
	if !acked || !moved {
		t.Fatalf("acked=%v moved=%v; camera state %+v", acked, moved, cam.Device())
	}
	if got := cam.AppliedLog(); len(got) != 1 || got[0].IP != "192.168.77.11" {
		t.Fatalf("applied log: %+v", got)
	}
}
