// Command mhedpcap inspects MHED traffic in a Wireshark capture (.pcapng or .pcap). A development tool for
// protocol work — e.g. checking that ipcprobe's packets match what IPTool sends — not part of releases.
//
//	mhedpcap list  capture.pcapng                 every MHED frame: number, endpoints, ttl, command, length
//	mhedpcap show  capture.pcapng --frame 2539    decode one frame (password field is never printed)
//	mhedpcap diff  capture.pcapng --frame 2539    rebuild a set-network frame with ipcprobe's builder and diff it
//
// Frame numbers are 1-based and match Wireshark's. Standard library only.
package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"

	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) < 2 {
		usage()
		return 2
	}
	verb, path := args[0], args[1]
	frameNo := 0
	for i := 2; i < len(args); i++ {
		if args[i] == "--frame" && i+1 < len(args) {
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				fmt.Fprintln(os.Stderr, "error: --frame wants a number")
				return 2
			}
			frameNo = n
			i++
		}
	}
	frames, err := readCapture(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	switch verb {
	case "list":
		return cmdList(frames)
	case "show", "diff":
		if frameNo == 0 {
			fmt.Fprintln(os.Stderr, "error: --frame N is required")
			return 2
		}
		f := find(frames, frameNo)
		if f == nil {
			fmt.Fprintf(os.Stderr, "error: frame %d is not an IPv4/UDP frame in this capture\n", frameNo)
			return 1
		}
		if verb == "show" {
			return cmdShow(f)
		}
		return cmdDiff(f)
	}
	usage()
	return 2
}

func usage() {
	fmt.Fprint(os.Stderr, `mhedpcap — inspect MHED frames in a .pcapng/.pcap capture

  mhedpcap list capture.pcapng
  mhedpcap show capture.pcapng --frame N
  mhedpcap diff capture.pcapng --frame N     (set-network frames only)
`)
}

// --- capture reading ---

type frame struct {
	no       int
	src, dst netip.Addr
	sport    uint16
	dport    uint16
	ttl      uint8
	payload  []byte // UDP payload
}

func find(fs []frame, no int) *frame {
	for i := range fs {
		if fs[i].no == no {
			return &fs[i]
		}
	}
	return nil
}

// readCapture returns every IPv4/UDP frame with its 1-based Wireshark frame number. Non-UDP frames are counted but
// not returned, so numbers stay aligned with Wireshark.
func readCapture(path string) ([]frame, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 4 {
		return nil, errors.New("file too short")
	}
	var raw [][2]interface{} // (linktype, packet bytes) — kept simple
	switch binary.LittleEndian.Uint32(data) {
	case 0x0A0D0D0A:
		raw, err = readPcapng(data)
	case 0xA1B2C3D4, 0xA1B23C4D:
		raw, err = readPcap(data, binary.LittleEndian)
	case 0xD4C3B2A1, 0x4D3CB2A1:
		raw, err = readPcap(data, binary.BigEndian)
	default:
		return nil, errors.New("not a pcap or pcapng file")
	}
	if err != nil {
		return nil, err
	}
	var out []frame
	for i, r := range raw {
		lt, pkt := r[0].(uint32), r[1].([]byte)
		f, ok := udpFrame(lt, pkt)
		if !ok {
			continue
		}
		f.no = i + 1
		out = append(out, f)
	}
	return out, nil
}

func readPcapng(data []byte) ([][2]interface{}, error) {
	var out [][2]interface{}
	var order binary.ByteOrder = binary.LittleEndian
	var linktypes []uint32
	pos := 0
	for pos+12 <= len(data) {
		btype := order.Uint32(data[pos:])
		if btype == 0x0A0D0D0A { // section header: byte-order magic decides endianness for this section
			if binary.LittleEndian.Uint32(data[pos+8:]) == 0x1A2B3C4D {
				order = binary.LittleEndian
			} else {
				order = binary.BigEndian
			}
			linktypes = nil
		}
		blen := int(order.Uint32(data[pos+4:]))
		if blen < 12 || pos+blen > len(data) {
			return nil, fmt.Errorf("corrupt pcapng block at offset %d", pos)
		}
		body := data[pos+8 : pos+blen-4]
		switch btype {
		case 0x00000001: // interface description
			linktypes = append(linktypes, uint32(order.Uint16(body)))
		case 0x00000006: // enhanced packet block
			ifidx, caplen := order.Uint32(body), int(order.Uint32(body[12:]))
			lt := uint32(1)
			if int(ifidx) < len(linktypes) {
				lt = linktypes[ifidx]
			}
			out = append(out, [2]interface{}{lt, body[20 : 20+caplen]})
		case 0x00000003: // simple packet block
			n := int(order.Uint32(body))
			lt := uint32(1)
			if len(linktypes) > 0 {
				lt = linktypes[0]
			}
			out = append(out, [2]interface{}{lt, body[4 : 4+n]})
		}
		pos += blen
	}
	return out, nil
}

func readPcap(data []byte, order binary.ByteOrder) ([][2]interface{}, error) {
	if len(data) < 24 {
		return nil, errors.New("pcap header too short")
	}
	lt := order.Uint32(data[20:])
	var out [][2]interface{}
	pos := 24
	for pos+16 <= len(data) {
		caplen := int(order.Uint32(data[pos+8:]))
		if pos+16+caplen > len(data) {
			break
		}
		out = append(out, [2]interface{}{lt, data[pos+16 : pos+16+caplen]})
		pos += 16 + caplen
	}
	return out, nil
}

func udpFrame(linktype uint32, pkt []byte) (frame, bool) {
	off := 0
	switch linktype {
	case 1: // Ethernet
		if len(pkt) < 14 {
			return frame{}, false
		}
		et := binary.BigEndian.Uint16(pkt[12:])
		off = 14
		if et == 0x8100 { // 802.1Q
			if len(pkt) < 18 {
				return frame{}, false
			}
			et = binary.BigEndian.Uint16(pkt[16:])
			off = 18
		}
		if et != 0x0800 {
			return frame{}, false
		}
	case 101: // raw IP
	case 0: // BSD loopback
		off = 4
	default:
		return frame{}, false
	}
	ip := pkt[off:]
	if len(ip) < 20 || ip[0]>>4 != 4 || ip[9] != 17 {
		return frame{}, false
	}
	ihl := int(ip[0]&0xF) * 4
	total := int(binary.BigEndian.Uint16(ip[2:]))
	if total > len(ip) || ihl+8 > total {
		return frame{}, false
	}
	udp := ip[ihl:total]
	ulen := int(binary.BigEndian.Uint16(udp[4:]))
	if ulen < 8 || ulen > len(udp) {
		return frame{}, false
	}
	src, _ := netip.AddrFromSlice(ip[12:16])
	dst, _ := netip.AddrFromSlice(ip[16:20])
	return frame{src: src, dst: dst, sport: binary.BigEndian.Uint16(udp), dport: binary.BigEndian.Uint16(udp[2:]),
		ttl: ip[8], payload: udp[8:ulen]}, true
}

// --- commands ---

func cmdList(frames []frame) int {
	n := 0
	for _, f := range frames {
		cmd, ok := mhed.Command(f.payload)
		if !ok {
			continue
		}
		n++
		fmt.Printf("frame %5d  %15s:%-5d -> %15s:%-5d  ttl %-3d  cmd 0x%02x %-11s len %d\n",
			f.no, f.src, f.sport, f.dst, f.dport, f.ttl, cmd, cmdName(cmd), len(f.payload))
	}
	fmt.Fprintf(os.Stderr, "%d MHED frame(s)\n", n)
	return 0
}

func cmdShow(f *frame) int {
	cmd, ok := mhed.Command(f.payload)
	if !ok {
		fmt.Fprintf(os.Stderr, "frame %d is UDP but not MHED\n", f.no)
		return 1
	}
	fmt.Printf("frame %d: %s:%d -> %s:%d  ttl %d  cmd 0x%02x (%s)  payload %d bytes\n",
		f.no, f.src, f.sport, f.dst, f.dport, f.ttl, cmd, cmdName(cmd), len(f.payload))
	switch cmd {
	case mhed.CmdAnnounce:
		d, ok := mhed.ParseAnnounce(f.payload)
		if !ok {
			fmt.Println("  (announce too short to parse)")
			return 1
		}
		b, _ := json.MarshalIndent(d, "  ", "  ")
		fmt.Println("  " + string(b))
	case mhed.CmdSetAck:
		ip, port, _ := mhed.ParseSetAck(f.payload)
		fmt.Printf("  requester %s:%d\n", ip, port)
	case mhed.CmdSetNet:
		s := decodeSet(f.payload)
		fmt.Printf("  mac %s  ip %s  mask %s  gw %s  dns %s,%s  password field: %d bytes (not shown)\n",
			s.mac, s.ip, s.mask, s.gw, s.dns1, s.dns2, s.pwLen)
	case mhed.CmdSearch:
		fmt.Println("  search probe (no payload)")
	}
	return 0
}

// cmdDiff rebuilds a captured set-network frame with mhed.BuildSetNetwork (empty password) and reports every
// differing byte outside the password field. Zero differences elsewhere means our builder matches the reference
// implementation byte for byte.
func cmdDiff(f *frame) int {
	if cmd, ok := mhed.Command(f.payload); !ok || cmd != mhed.CmdSetNet {
		fmt.Fprintf(os.Stderr, "frame %d is not a set-network (cmd 3) frame\n", f.no)
		return 1
	}
	s := decodeSet(f.payload)
	ours, err := mhed.BuildSetNetwork(s.mac, s.ip, s.mask, s.gw, s.dns1, s.dns2, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	const pwLo, pwHi = 84, 112
	var other, inPw int
	n := len(f.payload)
	if len(ours) > n {
		n = len(ours)
	}
	for i := 0; i < n; i++ {
		var a, b int = -1, -1
		if i < len(f.payload) {
			a = int(f.payload[i])
		}
		if i < len(ours) {
			b = int(ours[i])
		}
		if a == b {
			continue
		}
		if i >= pwLo && i < pwHi {
			inPw++
			continue
		}
		other++
		fmt.Printf("  offset %3d: capture %s  ours %s\n", i, hexOr(a), hexOr(b))
	}
	fmt.Printf("%d captured vs %d generated bytes: %d differ inside the password field, %d elsewhere\n",
		len(f.payload), len(ours), inPw, other)
	if other == 0 {
		fmt.Println("RESULT: MATCH (only the password field differs)")
		return 0
	}
	fmt.Println("RESULT: MISMATCH")
	return 1
}

type setFields struct {
	mac                      net.HardwareAddr
	ip, mask, gw, dns1, dns2 netip.Addr
	pwLen                    int
}

func decodeSet(p []byte) setFields {
	a := func(off int) netip.Addr { x, _ := netip.AddrFromSlice(p[off : off+4]); return x }
	pw := p[84:112]
	n := 0
	for n < len(pw) && pw[n] != 0 {
		n++
	}
	return setFields{mac: net.HardwareAddr(p[32:38]), ip: a(40), mask: a(44), gw: a(48), dns1: a(112), dns2: a(116), pwLen: n}
}

func cmdName(c uint16) string {
	switch c {
	case mhed.CmdSearch:
		return "search"
	case mhed.CmdAnnounce:
		return "announce"
	case mhed.CmdSetNet:
		return "set-network"
	case mhed.CmdSetAck:
		return "set-ack"
	}
	return "?"
}

func hexOr(v int) string {
	if v < 0 {
		return "--"
	}
	return fmt.Sprintf("%02x", v)
}
