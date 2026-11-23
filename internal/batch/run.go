package batch

import (
	"net"
	"time"

	"github.com/AlexandruIspas659/ipcprobe/internal/discovery"
	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

// Options controls how a batch is carried out.
type Options struct {
	// Canary sends to the first device alone and waits for it to confirm before touching the rest. If it does not
	// confirm, the rest are left alone (NotSent) — so a mistyped password reaches one camera, not the fleet.
	Canary bool
	// ConfirmTimeout is how long to wait for re-announcements after each round of sends.
	ConfirmTimeout time.Duration
	// Gap is the pause between two set-network datagrams of the same round.
	Gap time.Duration
}

// Run sends a set-network command to every Planned item and waits for each device to re-announce with its target
// configuration, updating the items' Status and Acked in place. All devices get the same password. Progress is
// reported through logf, which never receives the password. Run does nothing if the batch is Blocked.
func Run(conn *discovery.Conn, items []Item, password string, opt Options, logf func(format string, args ...any)) {
	if Blocked(items) {
		return
	}
	var todo []*Item
	for i := range items {
		if items[i].Status == Planned {
			todo = append(todo, &items[i])
		}
	}
	if len(todo) == 0 {
		return
	}
	rounds := [][]*Item{todo}
	canary := opt.Canary && len(todo) > 1
	if canary {
		rounds = [][]*Item{todo[:1], todo[1:]}
	}
	for r, round := range rounds {
		if canary && r == 0 {
			logf("testing on %s first; the other %d wait for it to confirm", describe(round[0]), len(todo)-1)
		}
		runRound(conn, round, password, opt, logf)
		if canary && r == 0 && round[0].Status != Confirmed {
			for _, it := range rounds[1] {
				it.Status = NotSent
				it.Error = "not sent: the first camera did not confirm"
			}
			logf("stopping: %d camera(s) were not sent anything", len(rounds[1]))
			return
		}
	}
}

func runRound(conn *discovery.Conn, round []*Item, password string, opt Options, logf func(string, ...any)) {
	sent := map[*Item]*discovery.Sent{}
	defer func() {
		for _, s := range sent {
			s.Close()
		}
	}()
	// An ack is matched to a device by the source port it echoes. As a fallback for firmware that might not echo
	// it faithfully, an ack from a device's old address also counts when no other device in the round shares it.
	oldIP := map[string]int{}
	for _, it := range round {
		oldIP[it.Current.IP.String()]++
	}

	for i, it := range round {
		if i > 0 && opt.Gap > 0 {
			time.Sleep(opt.Gap)
		}
		mac, _ := net.ParseMAC(it.MAC)
		t := it.Target
		pkt, err := mhed.BuildSetNetwork(mac, t.IP, t.Mask, t.Gateway, t.DNS1, t.DNS2, password)
		var s *discovery.Sent
		if err == nil {
			s, err = conn.SendSetTracked(pkt)
		}
		if err != nil {
			it.Status = NotSent
			it.Error = "not sent: " + err.Error()
			logf("could not send to %s: %v", describe(it), err)
			continue
		}
		sent[it] = s
		it.Status = NotConfirmed // until it re-announces
		logf("sent %s -> %s/%s gw %s dns %s,%s", describe(it), t.IP, t.Mask, t.Gateway, t.DNS1, t.DNS2)
	}

	pending := len(sent)
	deadline := time.Now().Add(opt.ConfirmTimeout)
	for pending > 0 && time.Now().Before(deadline) {
		_ = conn.Probe()
		window := 2 * time.Second
		if d := time.Until(deadline); d < window {
			window = d
		}
		if window < 100*time.Millisecond {
			window = 100 * time.Millisecond
		}
		devs, acks := conn.Collect(window)
		for _, it := range round {
			s := sent[it]
			if s == nil || it.Status == Confirmed {
				continue
			}
			if !it.Acked {
				for _, a := range acks {
					if s.Matches(a) || (a.From == it.Current.IP.String() && oldIP[a.From] == 1) {
						it.Acked = true
						logf("%s received the command (the ack does not validate the password)", describe(it))
						break
					}
				}
			}
			if it.Target.Matches(devs[it.MAC]) {
				it.Status = Confirmed
				pending--
				logf("confirmed: %s now announces at %s/%s gw %s", describe(it), it.Target.IP, it.Target.Mask, it.Target.Gateway)
			}
		}
	}
	for _, it := range round {
		if sent[it] == nil || it.Status == Confirmed {
			continue
		}
		if it.Acked {
			it.Error = "received the command but did not re-announce the new configuration: wrong admin password, or slow to come back"
		} else {
			it.Error = "no ack: the command probably never reached it (interface / VLAN?)"
		}
		logf("NOT confirmed: %s — %s", describe(it), it.Error)
	}
}

func describe(it *Item) string {
	if it.Name != "" {
		return it.MAC + " (" + it.Name + ")"
	}
	return it.MAC
}
