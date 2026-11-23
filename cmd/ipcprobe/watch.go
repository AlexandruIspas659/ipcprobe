package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AlexandruIspas659/ipcprobe/internal/discovery"
	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

// cmdWatch keeps listening and reports changes as they happen: cameras appearing, changing their configuration,
// and going quiet. It runs until interrupted.
//
// Without --json it prints one line per change. With --json it prints the whole device list, as one JSON object
// per line ({"devices":[...]}, the same objects as `list --json`), every time the list changes — and at least every
// ten seconds even if it does not, so that a program reading the stream can tell the watcher is alive, and the
// watcher notices when its reader has gone away. The macOS app's live list is exactly this stream.
func cmdWatch(args []string) int {
	f, err := parseFlags(args, map[string]bool{"json": true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	ifaces, err := discovery.Choose(f.str("iface", ""))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	conn, err := discovery.Open(ifaces)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	defer conn.Close()
	asJSON := f.has("json")
	if !asJSON {
		fmt.Fprintf(os.Stderr, "watching on %s — Ctrl-C to stop\n", ifaceList(ifaces))
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	const (
		window     = time.Second // how often changes are noticed
		probeEvery = 3           // a search every third window keeps devices announcing without flooding them
		heartbeat  = 10 * time.Second
	)
	tr := &discovery.Tracker{Expire: f.dur("expire", 15)}
	enc := json.NewEncoder(os.Stdout) // one compact object per line
	var lastEmit time.Time
	for n := 0; ; n++ {
		select {
		case <-stop:
			return 0
		default:
		}
		if n%probeEvery == 0 {
			_ = conn.Probe()
		}
		heard, _ := conn.Collect(window)
		now := time.Now()
		changes := tr.Observe(heard, now)

		if asJSON {
			// The first snapshot waits for a second window, so that a slow first answer does not make the list
			// start out half empty.
			if n == 0 || (len(changes) == 0 && now.Sub(lastEmit) < heartbeat && !lastEmit.IsZero()) {
				continue
			}
			snapshot := struct {
				Devices []*mhed.Device `json:"devices"`
			}{discovery.SortedByMAC(tr.Devices())}
			if snapshot.Devices == nil {
				snapshot.Devices = []*mhed.Device{}
			}
			if err := enc.Encode(snapshot); err != nil {
				return 0 // the reader is gone
			}
			lastEmit = now
			continue
		}
		for _, c := range changes {
			d := c.Device
			switch c.Kind {
			case discovery.Appeared:
				fmt.Printf("%s  + %s  %-15s  %s %q\n", stamp(now), d.MAC, d.IP, d.Model, d.Name)
			case discovery.Gone:
				fmt.Printf("%s  - %s  %-15s  %s %q  (silent for %s)\n", stamp(now), d.MAC, d.IP, d.Model, d.Name, tr.Expire)
			case discovery.Changed:
				fmt.Printf("%s  ~ %s  %s\n", stamp(now), d.MAC, describeChange(*c.Previous, d))
			}
		}
	}
}

func stamp(t time.Time) string { return t.Format("15:04:05") }

// describeChange lists the fields that differ between two announcements of one device, as "ip a -> b, …".
func describeChange(old, cur mhed.Device) string {
	var parts []string
	for _, f := range []struct{ name, a, b string }{
		{"ip", old.IP, cur.IP}, {"mask", old.Mask, cur.Mask}, {"gateway", old.Gateway, cur.Gateway},
		{"dns1", old.DNS1, cur.DNS1}, {"dns2", old.DNS2, cur.DNS2}, {"name", old.Name, cur.Name},
		{"firmware", old.Firmware, cur.Firmware}, {"http", fmt.Sprint(old.HTTP), fmt.Sprint(cur.HTTP)},
		{"rtsp", fmt.Sprint(old.RTSP), fmt.Sprint(cur.RTSP)},
	} {
		if f.a != f.b {
			parts = append(parts, fmt.Sprintf("%s %s -> %s", f.name, f.a, f.b))
		}
	}
	if len(parts) == 0 {
		return "announcement changed"
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += ", " + p
	}
	return out
}
