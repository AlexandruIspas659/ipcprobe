// Command ipcprobe discovers TVT/OEM (e.g. DVC) IP cameras on a LAN and sets their network config by MAC — a
// cross-platform replacement for the Windows "IPTool" / "IPC Manager" utility. See PROTOCOL.md.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/AlexandruIspas659/ipcprobe/internal/discovery"
	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

var version = "dev" // set via -ldflags "-X main.version=..."

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "list":
		return cmdList(args[1:])
	case "show":
		return cmdShow(args[1:])
	case "set":
		return cmdSet(args[1:])
	case "-h", "--help", "help":
		usage()
		return 0
	case "--version", "version":
		fmt.Println("ipcprobe", version)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `ipcprobe — discover and configure TVT/OEM IP cameras (MHED/UDP 23456)

  ipcprobe list [--iface en0] [--timeout 5] [--wide] [--json]
  ipcprobe show --mac <mac> [--iface en0] [--timeout 5] [--json]
  ipcprobe set  --mac <mac> --ip <ip> --mask <mask> --gw <gw>
                [--dns1 <ip>] [--dns2 <ip>] [--iface en0]
                [--password <pw>] [--force] [--timeout 3] [--confirm-timeout 8] [--dump]
`)
}

// --- flag helpers (stdlib flag is fine, but a tiny parser keeps --flag value / --flag=value uniform) ---

type flags struct {
	m   map[string]string
	set map[string]bool
}

func parseFlags(args []string, bools map[string]bool) (*flags, error) {
	f := &flags{m: map[string]string{}, set: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			return nil, fmt.Errorf("unexpected argument %q", a)
		}
		key := a[2:]
		if eq := strings.IndexByte(key, '='); eq >= 0 {
			f.m[key[:eq]] = key[eq+1:]
			f.set[key[:eq]] = true
			continue
		}
		if bools[key] {
			f.m[key] = "true"
			f.set[key] = true
			continue
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("flag --%s needs a value", key)
		}
		i++
		f.m[key] = args[i]
		f.set[key] = true
	}
	return f, nil
}

func (f *flags) str(k, def string) string {
	if v, ok := f.m[k]; ok {
		return v
	}
	return def
}
func (f *flags) has(k string) bool { return f.set[k] }
func (f *flags) dur(k string, def float64) time.Duration {
	s := f.str(k, "")
	if s == "" {
		return time.Duration(def * float64(time.Second))
	}
	var v float64
	fmt.Sscanf(s, "%g", &v)
	return time.Duration(v * float64(time.Second))
}

// --- commands ---

func cmdList(args []string) int {
	f, err := parseFlags(args, map[string]bool{"wide": true, "json": true})
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
	if !f.has("json") {
		fmt.Fprintf(os.Stderr, "listening on %s for %s ...\n", ifaceList(ifaces), f.dur("timeout", 5))
	}
	devs, _, _ := conn.Discover(f.dur("timeout", 5))
	if f.has("json") {
		printJSON(discovery.SortedByMAC(devs))
	} else {
		printTable(discovery.SortedByIP(devs), f.has("wide"))
		fmt.Fprintf(os.Stderr, "%d device(s)\n", len(devs))
	}
	return 0
}

func cmdShow(args []string) int {
	f, err := parseFlags(args, map[string]bool{"json": true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	mac, err := parseMAC(f.str("mac", ""))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	macText := mac.String()
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
	if !f.has("json") {
		fmt.Fprintf(os.Stderr, "listening on %s for %s ...\n", ifaceList(ifaces), f.dur("timeout", 5))
	}
	devs, _, _ := conn.Discover(f.dur("timeout", 5))
	dev := devs[macText]
	if dev == nil {
		if f.has("json") {
			fmt.Println("null")
		}
		fmt.Fprintf(os.Stderr, "error: %s not seen in discovery\n", macText)
		return 1
	}
	if f.has("json") {
		printJSON(dev)
		return 0
	}
	rows := [][2]string{
		{"mac", dev.MAC}, {"ip", dev.IP}, {"mask", dev.Mask}, {"gateway", dev.Gateway},
		{"dns1", dev.DNS1}, {"dns2", dev.DNS2}, {"name", dev.Name}, {"model", dev.Model},
		{"firmware", dev.Firmware}, {"build", dev.Build}, {"serial", dev.Serial}, {"vendor", dev.Vendor},
		{"http", fmt.Sprint(dev.HTTP)}, {"rtsp", fmt.Sprint(dev.RTSP)}, {"src", dev.Src},
	}
	if dev.HTTP != 0 {
		rows = append(rows, [2]string{"web", "http://" + dev.IP + portSuffix(dev.HTTP, 80) + "/"})
	}
	if dev.RTSP != 0 {
		rows = append(rows, [2]string{"rtsp-url", "rtsp://" + dev.IP + portSuffix(dev.RTSP, 554) + "/"})
	}
	w := 0
	for _, r := range rows {
		if len(r[0]) > w {
			w = len(r[0])
		}
	}
	for _, r := range rows {
		fmt.Printf("%*s : %s\n", w, r[0], r[1])
	}
	return 0
}

func cmdSet(args []string) int {
	f, err := parseFlags(args, map[string]bool{"force": true, "dump": true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	mac, err := parseMAC(f.str("mac", ""))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	macText := mac.String()
	ip, err1 := netip.ParseAddr(f.str("ip", ""))
	mask, err2 := netip.ParseAddr(f.str("mask", ""))
	gw, err3 := netip.ParseAddr(f.str("gw", ""))
	for _, e := range []error{err1, err2, err3} {
		if e != nil {
			fmt.Fprintln(os.Stderr, "error: --ip, --mask and --gw must all be valid IPv4 addresses")
			return 2
		}
	}
	prefix, ok := maskToPrefix(ip, mask)
	if !ok {
		fmt.Fprintf(os.Stderr, "error: %s is not a valid netmask\n", mask)
		return 2
	}
	if prefix.Bits() < 31 {
		net := prefix.Masked()
		if ip == net.Addr() || ip == broadcast(net) {
			fmt.Fprintf(os.Stderr, "error: %s is the network or broadcast address of %s\n", ip, net)
			return 2
		}
	}
	if !prefix.Contains(gw) {
		fmt.Fprintf(os.Stderr, "warning: gateway %s is outside %s\n", gw, prefix.Masked())
	}
	dns1 := gw
	if f.has("dns1") {
		if a, e := netip.ParseAddr(f.str("dns1", "")); e == nil {
			dns1 = a
		} else {
			fmt.Fprintln(os.Stderr, "error: bad --dns1")
			return 2
		}
	}
	dns2 := netip.MustParseAddr("8.8.8.8")
	if f.has("dns2") {
		if a, e := netip.ParseAddr(f.str("dns2", "")); e == nil {
			dns2 = a
		} else {
			fmt.Fprintln(os.Stderr, "error: bad --dns2")
			return 2
		}
	}

	password := f.str("password", "")
	if !f.has("password") {
		password, err = readPassword("camera admin password: ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "error reading password:", err)
			return 2
		}
	}
	if _, err := mhed.EncodePassword(password); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	ifaces, err := discovery.Choose(f.str("iface", ""), ip)
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
	before, _, _ := conn.Discover(f.dur("timeout", 3))
	if cur := before[macText]; cur != nil {
		fmt.Fprintf(os.Stderr, "found %s: %s %q fw %s at %s/%s gw %s\n",
			macText, cur.Model, cur.Name, cur.Firmware, cur.IP, cur.Mask, cur.Gateway)
	} else if !f.has("force") {
		fmt.Fprintf(os.Stderr, "error: %s not seen in discovery (%d other device(s) seen); use --force to send anyway\n",
			macText, len(before))
		return 1
	} else {
		fmt.Fprintf(os.Stderr, "warning: %s not seen in discovery, sending anyway (--force)\n", macText)
	}

	pkt, err := mhed.BuildSetNetwork(mac, ip, mask, gw, dns1, dns2, password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if f.has("dump") {
		fmt.Fprintln(os.Stderr, dumpMasked(pkt))
	}
	if err := conn.SendSet(pkt); err != nil {
		fmt.Fprintln(os.Stderr, "error sending:", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "sent set-network for %s -> %s/%s gw %s dns %s,%s via %s\n",
		macText, ip, mask, gw, dns1, dns2, ifaceNames(ifaces))

	// Confirm: an ack (receipt) then a re-announcement at the new IP.
	deadline := time.Now().Add(f.dur("confirm-timeout", 8))
	acked := false
	for time.Now().Before(deadline) {
		_ = conn.Probe()
		window := 2 * time.Second
		if d := time.Until(deadline); d < window {
			window = d
		}
		if window < 100*time.Millisecond {
			window = 100 * time.Millisecond
		}
		devs, acks := conn.Collect(window)
		if len(acks) > 0 && !acked {
			acked = true
			fmt.Fprintf(os.Stderr, "camera at %s received the command (the ack does not validate the password)\n",
				acks[0].From)
		}
		if d := devs[macText]; d != nil && d.IP == ip.String() {
			fmt.Printf("confirmed: %s now announces at %s/%s gw %s\n", macText, d.IP, d.Mask, d.Gateway)
			return 0
		}
	}
	hint := " — no ack from the camera: command probably never reached it (interface / VLAN?)"
	if acked {
		hint = " — the camera received the command but did not apply it: most likely a wrong admin password"
	}
	fmt.Fprintf(os.Stderr, "NOT confirmed: %s did not announce at %s within %s%s\n",
		macText, ip, f.dur("confirm-timeout", 8), hint)
	return 2
}

// --- rendering / parsing helpers ---

var baseCols = []struct{ head, key string }{
	{"MAC", "mac"}, {"IP", "ip"}, {"Mask", "mask"}, {"Gateway", "gateway"},
	{"Model", "model"}, {"Firmware", "firmware"}, {"Name", "name"},
}
var wideExtra = []struct{ head, key string }{{"HTTP", "http"}, {"RTSP", "rtsp"}, {"Build", "build"}}

func printTable(devs []*mhed.Device, wide bool) {
	cols := baseCols
	if wide {
		cols = append(append([]struct{ head, key string }{}, baseCols...), wideExtra...)
	}
	rows := make([][]string, 0, len(devs))
	for _, d := range devs {
		row := make([]string, len(cols))
		for i, c := range cols {
			row[i] = field(d, c.key)
		}
		rows = append(rows, row)
	}
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = len(c.head)
	}
	for _, r := range rows {
		for i, v := range r {
			if len(v) > widths[i] {
				widths[i] = len(v)
			}
		}
	}
	var head, sep []string
	for i, c := range cols {
		head = append(head, pad(c.head, widths[i]))
		sep = append(sep, strings.Repeat("-", widths[i]))
	}
	fmt.Println(strings.Join(head, "  "))
	fmt.Println(strings.Join(sep, "  "))
	for _, r := range rows {
		cells := make([]string, len(r))
		for i, v := range r {
			cells[i] = pad(v, widths[i])
		}
		fmt.Println(strings.Join(cells, "  "))
	}
}

func field(d *mhed.Device, key string) string {
	switch key {
	case "mac":
		return d.MAC
	case "ip":
		return d.IP
	case "mask":
		return d.Mask
	case "gateway":
		return d.Gateway
	case "model":
		return d.Model
	case "firmware":
		return d.Firmware
	case "name":
		return d.Name
	case "http":
		return fmt.Sprint(d.HTTP)
	case "rtsp":
		return fmt.Sprint(d.RTSP)
	case "build":
		return d.Build
	}
	return ""
}

func printJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

func pad(s string, w int) string { return s + strings.Repeat(" ", w-len(s)) }

func portSuffix(port, std uint16) string {
	if port == std {
		return ""
	}
	return fmt.Sprintf(":%d", port)
}

func dumpMasked(pkt []byte) string {
	const off, n = 84, 28
	out := make([]byte, len(pkt))
	copy(out, pkt)
	var b strings.Builder
	for i := 0; i < len(out); i++ {
		if i >= off && i < off+n {
			b.WriteString("**")
		} else {
			fmt.Fprintf(&b, "%02x", out[i])
		}
	}
	return b.String()
}

func ifaceList(ifaces []discovery.Iface) string {
	var s []string
	for _, i := range ifaces {
		s = append(s, i.String())
	}
	return strings.Join(s, ", ")
}
func ifaceNames(ifaces []discovery.Iface) string {
	var s []string
	for _, i := range ifaces {
		s = append(s, i.Name)
	}
	return strings.Join(s, ", ")
}

func parseMAC(s string) (net.HardwareAddr, error) {
	if s == "" {
		return nil, fmt.Errorf("--mac is required")
	}
	mac, err := net.ParseMAC(s)
	if err != nil || len(mac) != 6 {
		return nil, fmt.Errorf("invalid MAC address: %q", s)
	}
	return mac, nil
}

// maskToPrefix builds ip/prefix from a dotted netmask, verifying the mask is a valid contiguous netmask.
func maskToPrefix(ip, mask netip.Addr) (netip.Prefix, bool) {
	if !ip.Is4() || !mask.Is4() {
		return netip.Prefix{}, false
	}
	m := mask.As4()
	ones := net.IPv4Mask(m[0], m[1], m[2], m[3])
	bits, size := ones.Size() // size==0 means non-contiguous / invalid
	if size == 0 {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(ip, bits), true
}

func broadcast(p netip.Prefix) netip.Addr {
	a := p.Masked().Addr().As4()
	host := 32 - p.Bits()
	for i := 0; i < host; i++ {
		byteIdx := 3 - i/8
		a[byteIdx] |= 1 << (uint(i) % 8)
	}
	return netip.AddrFrom4(a)
}
