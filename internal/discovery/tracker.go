package discovery

import (
	"sort"
	"time"

	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

// Tracker turns a stream of discovery results into a picture of what is on the network now: devices appear when
// they are first heard, change when they announce something different, and are dropped when they have been silent
// for Expire. It has no clock and no sockets of its own — the caller passes in what Collect returned and when —
// which is what makes it testable.
type Tracker struct {
	// Expire is how long a device may stay silent before it is considered gone. Devices announce every couple of
	// seconds, but one that is being reconfigured goes quiet while it restarts its network.
	Expire time.Duration

	devices map[string]tracked
}

type tracked struct {
	dev  mhed.Device
	seen time.Time
}

// ChangeKind says what happened to a device.
type ChangeKind string

const (
	Appeared ChangeKind = "appeared"
	Changed  ChangeKind = "changed"
	Gone     ChangeKind = "gone"
)

// Change is one difference between two observations. Device is the device as it is now (as it last was, for Gone);
// Previous is what it announced before, for Changed.
type Change struct {
	Kind     ChangeKind
	Device   mhed.Device
	Previous *mhed.Device
}

// Observe folds in the devices heard during one collection window ending at now, and returns what changed, ordered
// by MAC.
func (t *Tracker) Observe(heard map[string]*mhed.Device, now time.Time) []Change {
	if t.devices == nil {
		t.devices = map[string]tracked{}
	}
	var changes []Change
	for mac, d := range heard {
		old, known := t.devices[mac]
		t.devices[mac] = tracked{dev: *d, seen: now}
		switch {
		case !known:
			changes = append(changes, Change{Kind: Appeared, Device: *d})
		case old.dev != *d:
			prev := old.dev
			changes = append(changes, Change{Kind: Changed, Device: *d, Previous: &prev})
		}
	}
	for mac, e := range t.devices {
		if now.Sub(e.seen) > t.Expire {
			delete(t.devices, mac)
			changes = append(changes, Change{Kind: Gone, Device: e.dev})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Device.MAC < changes[j].Device.MAC })
	return changes
}

// Devices returns the devices currently considered present, keyed by MAC.
func (t *Tracker) Devices() map[string]*mhed.Device {
	out := make(map[string]*mhed.Device, len(t.devices))
	for mac, e := range t.devices {
		d := e.dev
		out[mac] = &d
	}
	return out
}
