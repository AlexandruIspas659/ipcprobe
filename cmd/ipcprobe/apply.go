package main

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/AlexandruIspas659/ipcprobe/internal/batch"
	"github.com/AlexandruIspas659/ipcprobe/internal/discovery"
	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
	"github.com/AlexandruIspas659/ipcprobe/internal/neighbor"
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
	f, err := parseFlags(args, map[string]bool{"all": true, "rescue": true, "dry-run": true, "json": true, "csv": true, "yes": true, "no-canary": true, "no-probe": true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	fail := func(format string, a ...any) int {
		fmt.Fprintf(os.Stderr, "error: "+format+"\n", a...)
		return 2
	}
	dry, asJSON, asCSV := f.has("dry-run"), f.has("json"), f.has("csv")
	if asJSON && asCSV {
		return fail("choose one of --json and --csv")
	}

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
	var rescueIn batch.RescueInput
	var rescueMACs []string
	if rescue {
		// "This network" has to be one network: the subnet of the single interface in use.
		used := conn.Ifaces()
		if len(used) != 1 {
			return fail("--rescue brings cameras into this computer's network, and %d interfaces are in use (%s): choose one with --iface",
				len(used), ifaceNames(used))
		}
		for _, r := range reqs {
			rescueMACs = append(rescueMACs, r.MAC)
		}
		rescueIn = batch.RescueInput{Subnet: used[0].Net, Host: used[0].IP, Gateway: def.Gateway, DNS1: def.DNS1,
			DNS2: def.DNS2, Taken: map[netip.Addr]bool{}}
		fmt.Fprintf(os.Stderr, "bringing cameras into %s (%s)\n", used[0].Net, used[0].Name)
	}
	var seq []netip.Addr
	if startIP.IsValid() {
		if seq, err = batch.Sequence(startIP, step, len(reqs)); err != nil {
			return fail("%v", err)
		}
	}

	// Build the plan, then ask the network whether the addresses it hands out are really free (discovery only sees
	// cameras). For addresses the user chose, an occupied one becomes a warning on that camera. A rescue chooses
	// its own, so it simply chooses again without the occupied ones — a few rounds at most.
	var items []batch.Item
	for round := 1; ; round++ {
		gatewayAssumed := false
		if rescue {
			if reqs, gatewayAssumed, err = batch.Rescue(devs, rescueMACs, rescueIn); err != nil {
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
		items = batch.Build(devs, reqs)
		if gatewayAssumed {
			for i := range items {
				if it := &items[i]; it.Status == batch.Planned {
					it.Warnings = append(it.Warnings, fmt.Sprintf(
						"gateway %s is a guess: no camera on this network to copy it from (set it with --gw)", it.Target.Gateway))
				}
			}
		}
		if f.has("no-probe") {
			break
		}
		occupied := warnInUse(conn, devs, items)
		if !rescue || len(occupied) == 0 || round == 5 {
			break
		}
		for _, a := range occupied {
			rescueIn.Taken[a] = true
		}
	}
	out := applyOutput{DryRun: dry, Devices: items}
	report := func() {
		switch {
		case asJSON:
			printJSON(out)
		case asCSV:
			if err := writeCSV(os.Stdout, batchRows(items)); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
			}
		default:
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

// warnInUse checks the addresses the plan is about to hand out against the network itself, and adds a warning to
// every camera whose new address is held by something that is not a camera discovery saw (those are the plan's own
// business: batch.Build already refuses a clash between cameras). It returns the occupied addresses.
//
// Only addresses on this computer's own subnets can be checked; a move into a subnet it is not on goes unchecked.
// A failure to check is reported once and is not an error: the check is a courtesy, not a gate.
func warnInUse(conn *discovery.Conn, devs map[string]*mhed.Device, items []batch.Item) []netip.Addr {
	var addrs []netip.Addr
	for i := range items {
		if it := &items[i]; it.Status == batch.Planned && it.Target.IP != it.Current.IP && conn.OnLink(it.Target.IP) {
			addrs = append(addrs, it.Target.IP)
		}
	}
	if len(addrs) == 0 {
		return nil
	}
	owners, err := neighbor.InUse(addrs, 1200*time.Millisecond)
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: could not check whether the new addresses are free: %v\n", err)
		return nil
	}
	var occupied []netip.Addr
	for i := range items {
		it := &items[i]
		if it.Status != batch.Planned || it.Target.IP == it.Current.IP {
			continue
		}
		owner, ok := owners[it.Target.IP]
		if !ok || devs[owner.MAC.String()] != nil {
			continue
		}
		it.Warnings = append(it.Warnings, inUseWarning(it.Target.IP, owner))
		occupied = append(occupied, it.Target.IP)
	}
	return occupied
}

func inUseWarning(a netip.Addr, owner neighbor.Owner) string {
	if owner.Fresh {
		return fmt.Sprintf("%s is in use by %s, which is not a camera this tool can see", a, owner.MAC)
	}
	return fmt.Sprintf("%s was in use by %s within the last few minutes (not a camera this tool can see; it may have left since)", a, owner.MAC)
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
