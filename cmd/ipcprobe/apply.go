package main

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/AlexandruIspas659/ipcprobe/internal/batch"
	"github.com/AlexandruIspas659/ipcprobe/internal/discovery"
	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

// applyOutput is what `apply --json` prints: the plan (with --dry-run) or its outcome. The macOS app's batch sheet
// decodes exactly this.
type applyOutput struct {
	DryRun  bool         `json:"dry_run"`
	Devices []batch.Item `json:"devices"`
}

// cmdApply changes the network configuration of several cameras at once. See internal/batch for the rules; this
// function is argument handling, the preview, the confirmation prompt and the report.
//
// Exit codes follow `set`: 0 every camera confirmed (or already had the configuration), 1 setup or environment
// problem, 2 usage error, a plan that cannot be applied, or at least one camera not confirmed.
func cmdApply(args []string) int {
	f, err := parseFlags(args, map[string]bool{"all": true, "rescue": true, "dry-run": true, "json": true, "yes": true, "no-canary": true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	fail := func(format string, a ...any) int {
		fmt.Fprintf(os.Stderr, "error: "+format+"\n", a...)
		return 2
	}
	dry, asJSON := f.has("dry-run"), f.has("json")

	selectors := 0
	for _, k := range []string{"mac", "all", "plan"} {
		if f.has(k) {
			selectors++
		}
	}
	if selectors != 1 {
		return fail("choose the cameras with exactly one of --mac <mac>,<mac>,..., --all or --plan <file>")
	}

	// What the flags ask for. A zero address means "not given": keep each camera's current value.
	var def batch.Config
	for _, o := range []struct {
		flag string
		dst  *netip.Addr
	}{{"mask", &def.Mask}, {"gw", &def.Gateway}, {"dns1", &def.DNS1}, {"dns2", &def.DNS2}} {
		if !f.has(o.flag) {
			continue
		}
		a, err := netip.ParseAddr(f.str(o.flag, ""))
		if err != nil || !a.Is4() {
			return fail("--%s must be an IPv4 address", o.flag)
		}
		*o.dst = a
	}
	var startIP netip.Addr
	step := 1
	if f.has("start-ip") {
		if f.has("plan") {
			return fail("--start-ip cannot be combined with --plan (the plan gives each camera its address)")
		}
		a, err := netip.ParseAddr(f.str("start-ip", ""))
		if err != nil || !a.Is4() {
			return fail("--start-ip must be an IPv4 address")
		}
		startIP = a
	}
	if f.has("step") {
		n, err := strconv.Atoi(f.str("step", ""))
		if err != nil || n < 1 || !startIP.IsValid() {
			return fail("--step must be a positive number and needs --start-ip")
		}
		step = n
	}
	rescue := f.has("rescue")
	if rescue {
		for _, k := range []string{"start-ip", "plan", "mask"} {
			if f.has(k) {
				return fail("--rescue works out the addresses and the mask itself; it cannot be combined with --%s", k)
			}
		}
	}
	if f.has("ip") {
		return fail("apply has no --ip; use --start-ip to hand out consecutive addresses, or `set` for one camera")
	}

	var reqs []batch.Request
	switch {
	case f.has("mac"):
		for _, m := range strings.Split(f.str("mac", ""), ",") {
			if m = strings.TrimSpace(m); m == "" {
				continue
			}
			hw, err := parseMAC(m)
			if err != nil {
				return fail("%v", err)
			}
			reqs = append(reqs, batch.Request{MAC: hw.String()})
		}
		if len(reqs) == 0 {
			return fail("--mac needs at least one MAC address")
		}
	case f.has("plan"):
		if reqs, err = readPlan(f.str("plan", "")); err != nil {
			return fail("%v", err)
		}
	}

	// A batch is never applied on the strength of a piped password alone: a wrapper has to preview with --dry-run
	// and then say --yes. On a terminal the preview is shown and confirmed interactively.
	if !dry && !f.has("yes") && !stdinIsTerminal() {
		return fail("stdin is not a terminal: preview with --dry-run, then pass --yes to apply")
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

	fmt.Fprintf(os.Stderr, "discovering on %s ...\n", ifaceList(ifaces))
	devs, _, _ := conn.Discover(f.dur("timeout", 5))

	if f.has("all") {
		for _, d := range discovery.SortedByIP(devs) {
			if rescue && !d.Foreign { // --all --rescue means every camera that needs rescuing
				continue
			}
			reqs = append(reqs, batch.Request{MAC: d.MAC})
		}
		if len(reqs) == 0 && !rescue {
			fmt.Fprintln(os.Stderr, "error: no devices seen in discovery")
			return 1
		}
	}
	gatewayAssumed := false
	if rescue {
		// "This network" has to be one network: the subnet of the single interface in use.
		used := conn.Ifaces()
		if len(used) != 1 {
			return fail("--rescue brings cameras into this computer's network, and %d interfaces are in use (%s): choose one with --iface",
				len(used), ifaceNames(used))
		}
		macs := make([]string, len(reqs))
		for i, r := range reqs {
			macs[i] = r.MAC
		}
		in := batch.RescueInput{Subnet: used[0].Net, Host: used[0].IP, Gateway: def.Gateway, DNS1: def.DNS1, DNS2: def.DNS2}
		if reqs, gatewayAssumed, err = batch.Rescue(devs, macs, in); err != nil {
			return fail("%v", err)
		}
		fmt.Fprintf(os.Stderr, "bringing cameras into %s (%s)\n", used[0].Net, used[0].Name)
	}
	var seq []netip.Addr
	if startIP.IsValid() {
		if seq, err = batch.Sequence(startIP, step, len(reqs)); err != nil {
			return fail("%v", err)
		}
	}
	for i := range reqs {
		r := &reqs[i]
		if seq != nil {
			r.IP = seq[i]
		}
		// A plan entry's own value wins over a flag; a flag wins over the camera's current value.
		for _, o := range []struct{ dst, def *netip.Addr }{
			{&r.Mask, &def.Mask}, {&r.Gateway, &def.Gateway}, {&r.DNS1, &def.DNS1}, {&r.DNS2, &def.DNS2}} {
			if !o.dst.IsValid() {
				*o.dst = *o.def
			}
		}
	}

	items := batch.Build(devs, reqs)
	if gatewayAssumed {
		for i := range items {
			if it := &items[i]; it.Status == batch.Planned {
				it.Warnings = append(it.Warnings, fmt.Sprintf(
					"gateway %s is a guess: no camera on this network to copy it from (set it with --gw)", it.Target.Gateway))
			}
		}
	}
	out := applyOutput{DryRun: dry, Devices: items}
	report := func() {
		if asJSON {
			printJSON(out)
		} else {
			printBatch(os.Stdout, items)
		}
	}
	planned := batch.Count(items, batch.Planned)

	switch {
	case batch.Blocked(items):
		report()
		fmt.Fprintf(os.Stderr, "error: %d camera(s) cannot be configured as asked (see above); nothing was sent\n",
			batch.Count(items, batch.Invalid))
		return 2
	case dry:
		report()
		fmt.Fprintf(os.Stderr, "dry run: %d to change, %d already as requested, %d not seen; nothing was sent\n",
			planned, batch.Count(items, batch.Unchanged), batch.Count(items, batch.NotSeen))
		return 0
	case planned == 0:
		report()
		fmt.Fprintln(os.Stderr, "nothing to change")
		return exitFor(items)
	}

	if !asJSON {
		printBatch(os.Stderr, items) // the preview; stdout is kept for the outcome
	}
	password := f.str("password", "")
	if !f.has("password") {
		password, err = readPassword(fmt.Sprintf("admin password (the same for all %d cameras): ", planned))
		if err != nil {
			return fail("reading password: %v", err)
		}
	}
	if _, err := mhed.EncodePassword(password); err != nil {
		return fail("%v", err)
	}
	if !f.has("yes") {
		fmt.Fprintf(os.Stderr, "change the network configuration of %d camera(s)? [y/N] ", planned)
		answer, _ := readLine()
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Fprintln(os.Stderr, "cancelled; nothing was sent")
			return 2
		}
	}

	batch.Run(conn, items, password, batch.Options{
		Canary:         !f.has("no-canary"),
		ConfirmTimeout: f.dur("confirm-timeout", 15),
		Gap:            f.dur("gap", 0.25),
	}, func(format string, a ...any) { fmt.Fprintf(os.Stderr, format+"\n", a...) })

	report()
	fmt.Fprintf(os.Stderr, "%d confirmed, %d not confirmed, %d not sent, %d unchanged, %d not seen\n",
		batch.Count(items, batch.Confirmed), batch.Count(items, batch.NotConfirmed), batch.Count(items, batch.NotSent),
		batch.Count(items, batch.Unchanged), batch.Count(items, batch.NotSeen))
	return exitFor(items)
}

// exitFor is 0 when every camera in the batch ended up with its target configuration, 2 otherwise.
func exitFor(items []batch.Item) int {
	for i := range items {
		if s := items[i].Status; s != batch.Confirmed && s != batch.Unchanged {
			return 2
		}
	}
	return 0
}

// readPlan reads a JSON array of {"mac","ip","mask","gateway","dns1","dns2"} objects. Every field but mac is
// optional; other keys are ignored, so the output of `list --json` can be edited and fed straight back in. A plan
// never contains a password.
func readPlan(path string) ([]batch.Request, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []struct {
		MAC     string `json:"mac"`
		IP      string `json:"ip"`
		Mask    string `json:"mask"`
		Gateway string `json:"gateway"`
		DNS1    string `json:"dns1"`
		DNS2    string `json:"dns2"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("%s: expected a JSON array of {\"mac\", \"ip\", \"mask\", \"gateway\", \"dns1\", \"dns2\"} objects: %v", path, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s: the plan is empty", path)
	}
	reqs := make([]batch.Request, 0, len(entries))
	for i, e := range entries {
		hw, err := parseMAC(e.MAC)
		if err != nil {
			return nil, fmt.Errorf("%s: entry %d: %v", path, i+1, err)
		}
		r := batch.Request{MAC: hw.String()}
		for _, o := range []struct {
			name, s string
			dst     *netip.Addr
		}{{"ip", e.IP, &r.IP}, {"mask", e.Mask, &r.Mask}, {"gateway", e.Gateway, &r.Gateway}, {"dns1", e.DNS1, &r.DNS1}, {"dns2", e.DNS2, &r.DNS2}} {
			if o.s == "" {
				continue
			}
			a, err := netip.ParseAddr(o.s)
			if err != nil || !a.Is4() {
				return nil, fmt.Errorf("%s: entry %d (%s): %s %q is not an IPv4 address", path, i+1, hw, o.name, o.s)
			}
			*o.dst = a
		}
		reqs = append(reqs, r)
	}
	return reqs, nil
}

// printBatch renders the plan or its outcome as a table: one row per camera, "old -> new" where a field changes,
// and each camera's warnings and error on indented lines below its row.
func printBatch(w *os.File, items []batch.Item) {
	change := func(it *batch.Item, pick func(batch.Config) netip.Addr) string {
		switch {
		case it.Current == nil:
			return "?"
		case it.Target == nil || pick(*it.Current) == pick(*it.Target):
			return pick(*it.Current).String()
		}
		return pick(*it.Current).String() + " -> " + pick(*it.Target).String()
	}
	head := []string{"MAC", "Name", "IP", "Mask", "Gateway", "DNS 1", "DNS 2", "Status"}
	rows := make([][]string, len(items))
	for i := range items {
		it := &items[i]
		status := string(it.Status)
		if it.Status == batch.NotConfirmed && it.Acked {
			status += " (acked)"
		}
		rows[i] = []string{it.MAC, it.Name,
			change(it, func(c batch.Config) netip.Addr { return c.IP }),
			change(it, func(c batch.Config) netip.Addr { return c.Mask }),
			change(it, func(c batch.Config) netip.Addr { return c.Gateway }),
			change(it, func(c batch.Config) netip.Addr { return c.DNS1 }),
			change(it, func(c batch.Config) netip.Addr { return c.DNS2 }),
			status}
	}
	widths := make([]int, len(head))
	for i, h := range head {
		widths[i] = len(h)
	}
	for _, r := range rows {
		for i, v := range r {
			if len(v) > widths[i] {
				widths[i] = len(v)
			}
		}
	}
	line := func(cells []string) {
		padded := make([]string, len(cells))
		for i, v := range cells {
			padded[i] = pad(v, widths[i])
		}
		fmt.Fprintln(w, strings.TrimRight(strings.Join(padded, "  "), " "))
	}
	line(head)
	sep := make([]string, len(head))
	for i := range sep {
		sep[i] = strings.Repeat("-", widths[i])
	}
	line(sep)
	for i, r := range rows {
		line(r)
		for _, warn := range items[i].Warnings {
			fmt.Fprintf(w, "    warning: %s\n", warn)
		}
		if items[i].Error != "" {
			fmt.Fprintf(w, "    %s\n", items[i].Error)
		}
	}
}
