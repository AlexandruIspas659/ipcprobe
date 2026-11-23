package batch

import (
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/AlexandruIspas659/ipcprobe/internal/discovery"
	"github.com/AlexandruIspas659/ipcprobe/internal/fakecam"
	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

// rig is a handful of fake cameras and a discovery connection on the first usable interface. Like the discovery
// end-to-end test, everything here goes over real multicast sockets and skips itself where that is unavailable.
//
// The cameras use MACs and a subnet of their own (02:00:00:00:01:xx, 192.168.88.x) because Go runs the packages'
// tests in parallel on the same interface, and the discovery test has a camera up at the same time.
type rig struct {
	conn *discovery.Conn
	cams []*fakecam.Camera
	macs []string
}

func startRig(t *testing.T, passwords ...string) *rig {
	t.Helper()
	if testing.Short() {
		t.Skip("-short")
	}
	all, err := discovery.Interfaces()
	if err != nil || len(all) == 0 {
		t.Skip("no multicast-capable IPv4 interface")
	}
	ifc := all[0]
	ifi, err := discovery.NetInterface(ifc)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{}
	for i, pw := range passwords {
		mac := fmt.Sprintf("02:00:00:00:01:%02x", i+1)
		cam, err := fakecam.Start(ifi, ifc.IP, fakecam.Config{
			Device: mhed.Device{
				MAC: mac, IP: fmt.Sprintf("192.168.88.%d", 10+i), Mask: "255.255.255.0", Gateway: "192.168.88.1",
				DNS1: "192.168.88.1", DNS2: "8.8.8.8", Name: fmt.Sprintf("batch-%d", i+1), Serial: "BATCH0000001",
				Firmware: "1.5-1", Model: "DCN-TEST", Vendor: "TEST", HTTP: 80, RTSP: 554, Build: "2026-01-02",
			},
			Password: pw,
			Interval: 200 * time.Millisecond,
		}, t.Logf)
		if err != nil {
			t.Skipf("fake camera could not start on %s: %v", ifc, err)
		}
		t.Cleanup(cam.Close)
		r.cams = append(r.cams, cam)
		r.macs = append(r.macs, mac)
	}
	if r.conn, err = discovery.Open([]discovery.Iface{ifc}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.conn.Close)
	return r
}

// discover returns only the rig's own cameras: whatever else is announcing on this network (real cameras
// included) is none of the test's business and must never end up in a plan.
func (r *rig) discover(t *testing.T) map[string]*mhed.Device {
	t.Helper()
	seen, _, err := r.conn.Discover(1500 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	devs := map[string]*mhed.Device{}
	for _, mac := range r.macs {
		if d := seen[mac]; d != nil {
			devs[mac] = d
		}
	}
	if len(devs) != len(r.macs) {
		t.Skipf("heard %d of %d fake cameras — multicast loopback not available here", len(devs), len(r.macs))
	}
	return devs
}

func (r *rig) requests(cfg func(i int) Config) []Request {
	reqs := make([]Request, len(r.macs))
	for i, mac := range r.macs {
		reqs[i] = Request{MAC: mac, Config: cfg(i)}
	}
	return reqs
}

var testOptions = Options{Canary: true, ConfirmTimeout: 4 * time.Second, Gap: 50 * time.Millisecond}

func TestRunSequentialAddresses(t *testing.T) {
	r := startRig(t, "s3cret", "s3cret", "s3cret")
	seq, _ := Sequence(netip.MustParseAddr("192.168.88.100"), 1, 3)
	items := Build(r.discover(t), r.requests(func(i int) Config { return Config{IP: seq[i]} }))
	if Blocked(items) || Count(items, Planned) != 3 {
		t.Fatalf("plan: %+v", items)
	}
	Run(r.conn, items, "s3cret", testOptions, t.Logf)
	for i, it := range items {
		if it.Status != Confirmed || !it.Acked {
			t.Errorf("%s: status %s acked %v (%s), want confirmed and acked", it.MAC, it.Status, it.Acked, it.Error)
		}
		if got := r.cams[i].Device().IP; got != seq[i].String() {
			t.Errorf("%s: camera is at %s, want %s", it.MAC, got, seq[i])
		}
		if n := r.cams[i].Received(); n != 1 {
			t.Errorf("%s: received %d commands, want exactly 1", it.MAC, n)
		}
	}
}

// A wrong password must reach the first camera only, and must not be reported as a success there.
func TestRunStopsWhenTheFirstCameraDoesNotConfirm(t *testing.T) {
	r := startRig(t, "s3cret", "s3cret", "s3cret")
	seq, _ := Sequence(netip.MustParseAddr("192.168.88.100"), 1, 3)
	items := Build(r.discover(t), r.requests(func(i int) Config { return Config{IP: seq[i]} }))
	opt := testOptions
	opt.ConfirmTimeout = 2500 * time.Millisecond
	Run(r.conn, items, "wrong", opt, t.Logf)

	if items[0].Status != NotConfirmed || !items[0].Acked {
		t.Errorf("first camera: status %s acked %v, want not-confirmed and acked", items[0].Status, items[0].Acked)
	}
	for i := 1; i < len(items); i++ {
		if items[i].Status != NotSent {
			t.Errorf("%s: status %s, want not-sent", items[i].MAC, items[i].Status)
		}
		if n := r.cams[i].Received(); n != 0 {
			t.Errorf("%s: received %d commands after the first camera failed, want 0", items[i].MAC, n)
		}
	}
	for i, cam := range r.cams {
		if got, want := cam.Device().IP, fmt.Sprintf("192.168.88.%d", 10+i); got != want {
			t.Errorf("camera %d moved to %s on a wrong password", i, got)
		}
	}
}

// A change that keeps every address: the cameras announce at the "right" IP throughout, so only the gateway can
// tell applied from ignored. One camera has a different password and must come back not-confirmed — and, because
// each ack is matched to its own datagram, acked.
func TestRunGatewayInPlaceIsNotConfirmedByTheUnchangedAddress(t *testing.T) {
	r := startRig(t, "s3cret", "s3cret", "other")
	gw := Config{Gateway: netip.MustParseAddr("192.168.88.254")}
	items := Build(r.discover(t), r.requests(func(int) Config { return gw }))
	if Count(items, Planned) != 3 {
		t.Fatalf("plan: %+v", items)
	}
	opt := testOptions
	opt.ConfirmTimeout = 2500 * time.Millisecond
	Run(r.conn, items, "s3cret", opt, t.Logf)

	for i, want := range []Status{Confirmed, Confirmed, NotConfirmed} {
		if items[i].Status != want || !items[i].Acked {
			t.Errorf("%s: status %s acked %v, want %s and acked", items[i].MAC, items[i].Status, items[i].Acked, want)
		}
	}
	if got := r.cams[2].Device().Gateway; got != "192.168.88.1" {
		t.Errorf("the camera with the other password changed its gateway to %s", got)
	}
	if got := r.cams[0].Device(); got.Gateway != "192.168.88.254" || got.IP != "192.168.88.10" {
		t.Errorf("first camera: %s gw %s, want 192.168.88.10 gw 192.168.88.254", got.IP, got.Gateway)
	}
}
