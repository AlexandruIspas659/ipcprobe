package discovery

import (
	"testing"
	"time"

	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

func heard(ds ...mhed.Device) map[string]*mhed.Device {
	m := map[string]*mhed.Device{}
	for i := range ds {
		m[ds[i].MAC] = &ds[i]
	}
	return m
}

func kinds(cs []Change) string {
	s := ""
	for _, c := range cs {
		s += string(c.Kind) + ":" + c.Device.MAC[15:] + " "
	}
	return s
}

func TestTracker(t *testing.T) {
	a := mhed.Device{MAC: "02:00:00:00:00:0a", IP: "192.168.1.10", Name: "a"}
	b := mhed.Device{MAC: "02:00:00:00:00:0b", IP: "192.168.1.11", Name: "b"}
	tr := &Tracker{Expire: 10 * time.Second}
	t0 := time.Unix(1_800_000_000, 0)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }

	if got := kinds(tr.Observe(heard(a, b), at(0))); got != "appeared:0a appeared:0b " {
		t.Fatalf("first window: %q", got)
	}
	// Hearing the same thing again is not news.
	if got := tr.Observe(heard(a, b), at(2)); len(got) != 0 {
		t.Fatalf("unchanged window reported %v", got)
	}
	// A moves; B is silent for a window — silence alone is nothing yet.
	moved := a
	moved.IP = "192.168.1.50"
	cs := tr.Observe(heard(moved), at(4))
	if kinds(cs) != "changed:0a " || cs[0].Previous == nil || cs[0].Previous.IP != "192.168.1.10" || cs[0].Device.IP != "192.168.1.50" {
		t.Fatalf("move: %+v", cs)
	}
	if len(tr.Devices()) != 2 || tr.Devices()[a.MAC].IP != "192.168.1.50" {
		t.Fatalf("devices after the move: %+v", tr.Devices())
	}
	// B was last heard at 2 s: still there at 12 s (10 s of silence is the limit, not past it) ...
	if got := tr.Observe(heard(moved), at(12)); len(got) != 0 {
		t.Fatalf("B dropped too early: %v", got)
	}
	// ... and gone at 13 s, reported with what it last announced.
	cs = tr.Observe(heard(moved), at(13))
	if kinds(cs) != "gone:0b " || cs[0].Device.IP != "192.168.1.11" {
		t.Fatalf("expiry: %+v", cs)
	}
	if _, still := tr.Devices()[b.MAC]; still {
		t.Fatal("B is still listed after expiring")
	}
	// When it comes back it is new again.
	if got := kinds(tr.Observe(heard(moved, b), at(20))); got != "appeared:0b " {
		t.Fatalf("return: %q", got)
	}
	// An empty window (nothing heard at all) expires everything in the end, in MAC order.
	if got := kinds(tr.Observe(nil, at(40))); got != "gone:0a gone:0b " {
		t.Fatalf("everything gone: %q", got)
	}
	// Devices hands out copies: changing one must not change the tracker's view.
	tr.Observe(heard(a), at(41))
	tr.Devices()[a.MAC].IP = "tampered"
	if tr.Devices()[a.MAC].IP != "192.168.1.10" {
		t.Fatal("Devices exposes the tracker's own state")
	}
}
