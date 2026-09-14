package mhed

import (
	"encoding/hex"
	"encoding/json"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

type vectorFile struct {
	Vectors []struct {
		Name      string     `json:"name"`
		Cmd       int        `json:"cmd"`
		Hex       string     `json:"hex"`
		Expect    *Device    `json:"expect"`
		ExpectAck *ackExpect `json:"expect_ack"`
		DecodesTo *setDecode `json:"decodes_to"`
	} `json:"vectors"`
}

type ackExpect struct {
	IP   string `json:"requester_ip"`
	Port uint16 `json:"requester_port"`
}

type setDecode struct {
	MAC, IP, Mask, Gateway, DNS1, DNS2, Password string
}

func loadVectors(t *testing.T) vectorFile {
	t.Helper()
	p := filepath.Join("..", "..", "testdata", "vectors.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var vf vectorFile
	if err := json.Unmarshal(b, &vf); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	if len(vf.Vectors) == 0 {
		t.Fatal("no vectors")
	}
	return vf
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex: %v", err)
	}
	return b
}

func addr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatalf("bad addr %q: %v", s, err)
	}
	return a
}

// The central contract: every vector in testdata/vectors.json must round-trip.
func TestVectors(t *testing.T) {
	vf := loadVectors(t)
	for _, v := range vf.Vectors {
		v := v
		t.Run(v.Name, func(t *testing.T) {
			pkt := mustHex(t, v.Hex)
			switch v.Cmd {
			case CmdAnnounce:
				got, ok := ParseAnnounce(pkt)
				if !ok {
					t.Fatal("ParseAnnounce returned ok=false")
				}
				if *got != *v.Expect {
					t.Errorf("mismatch\n got: %+v\nwant: %+v", *got, *v.Expect)
				}
			case CmdSetAck:
				ip, port, ok := ParseSetAck(pkt)
				if !ok || ip != v.ExpectAck.IP || port != v.ExpectAck.Port {
					t.Errorf("got %s:%d ok=%v, want %s:%d", ip, port, ok, v.ExpectAck.IP, v.ExpectAck.Port)
				}
			case CmdSearch:
				if got := BuildSearch(); !equal(got, pkt) {
					t.Errorf("BuildSearch != vector")
				}
			case CmdSetNet:
				d := v.DecodesTo
				mac, err := net.ParseMAC(d.MAC)
				if err != nil {
					t.Fatalf("mac: %v", err)
				}
				got, err := BuildSetNetwork(mac, addr(t, d.IP), addr(t, d.Mask), addr(t, d.Gateway),
					addr(t, d.DNS1), addr(t, d.DNS2), d.Password)
				if err != nil {
					t.Fatalf("build: %v", err)
				}
				if !equal(got, pkt) {
					t.Errorf("BuildSetNetwork != vector\n got: %x\nwant: %x", got, pkt)
				}
			default:
				t.Fatalf("unknown cmd %d", v.Cmd)
			}
		})
	}
}

// The announce vectors have every undocumented byte zeroed, so building from the decoded struct must reproduce the
// exact bytes — BuildAnnounce and ParseAnnounce are true inverses of each other.
func TestAnnounceRoundTrip(t *testing.T) {
	vf := loadVectors(t)
	n := 0
	for _, v := range vf.Vectors {
		if v.Cmd != CmdAnnounce {
			continue
		}
		n++
		pkt := mustHex(t, v.Hex)
		built, err := BuildAnnounce(*v.Expect)
		if err != nil {
			t.Fatalf("%s: BuildAnnounce: %v", v.Name, err)
		}
		if !equal(built, pkt) {
			t.Errorf("%s: BuildAnnounce(ParseAnnounce(x)) != x\n got: %x\nwant: %x", v.Name, built, pkt)
		}
		back, ok := ParseAnnounce(built)
		if !ok || *back != *v.Expect {
			t.Errorf("%s: parse(build) mismatch: %+v", v.Name, back)
		}
	}
	if n == 0 {
		t.Fatal("no announce vectors")
	}
}

func TestSetAckRoundTrip(t *testing.T) {
	vf := loadVectors(t)
	for _, v := range vf.Vectors {
		if v.Cmd != CmdSetAck {
			continue
		}
		built, err := BuildSetAck(a4(v.ExpectAck.IP), v.ExpectAck.Port)
		if err != nil {
			t.Fatal(err)
		}
		if !equal(built, mustHex(t, v.Hex)) {
			t.Errorf("BuildSetAck != vector\n got: %x\nwant: %x", built, mustHex(t, v.Hex))
		}
	}
}

func TestBuildAnnounceRejectsBadInput(t *testing.T) {
	_, err := BuildAnnounce(Device{MAC: "nope"})
	if err == nil {
		t.Error("bad MAC accepted")
	}
	_, err = BuildAnnounce(Device{MAC: "02:00:00:00:00:01", IP: "1.2.3.4", Mask: "255.255.255.0", Gateway: "1.2.3.1",
		DNS1: "1.2.3.1", DNS2: "8.8.8.8", Name: "this name is far too long for twenty bytes"})
	if err == nil {
		t.Error("overlong name accepted")
	}
}

func TestSearchLayout(t *testing.T) {
	p := BuildSearch()
	if len(p) != 140 {
		t.Fatalf("len %d", len(p))
	}
	if got := hex.EncodeToString(p[:10]); got != "4d484544090001000100" {
		t.Errorf("header %s", got)
	}
}

func TestPasswordLimit(t *testing.T) {
	if _, err := EncodePassword("012345678901234567890"); err != nil { // 21 bytes -> fits
		t.Errorf("21-byte password should fit: %v", err)
	}
	if _, err := EncodePassword("0123456789012345678901"); err == nil { // 22 bytes -> too long
		t.Error("22-byte password should be rejected")
	}
}

func TestEmptyPasswordIsZeroField(t *testing.T) {
	mac, _ := net.ParseMAC("02:00:00:00:00:01")
	pkt, err := BuildSetNetwork(mac, a4("192.168.1.68"), a4("255.255.255.0"), a4("192.168.1.1"),
		a4("192.168.1.1"), a4("8.8.8.8"), "")
	if err != nil {
		t.Fatal(err)
	}
	for i := setPassword; i < setPassword+lenPassword; i++ {
		if pkt[i] != 0 {
			t.Fatalf("password field not zero at %d", i)
		}
	}
}

func TestBuildDate(t *testing.T) {
	pkt := make([]byte, lenAnnounce)
	copy(pkt, header(CmdAnnounce))
	// header() writes verTX; announce actually carries verRX, but Command only checks cmd, so this is fine here.
	putU16(pkt[8:], CmdAnnounce)
	putU32(pkt[offBuild:], 20231103)
	d, ok := ParseAnnounce(pkt)
	if !ok || d.Build != "2023-11-03" {
		t.Fatalf("build=%q ok=%v", d.Build, ok)
	}
	putU32(pkt[offBuild:], 0)
	d, _ = ParseAnnounce(pkt)
	if d.Build != "" {
		t.Fatalf("build=%q, want empty", d.Build)
	}
}

func TestRejectsOther(t *testing.T) {
	if _, ok := ParseAnnounce(BuildSearch()); ok {
		t.Error("search parsed as announce")
	}
	if _, ok := ParseAnnounce([]byte("junk")); ok {
		t.Error("junk parsed as announce")
	}
}

func a4(s string) netip.Addr    { a, _ := netip.ParseAddr(s); return a }
func putU16(b []byte, v uint16) { b[0] = byte(v); b[1] = byte(v >> 8) }
func putU32(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
}

func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
