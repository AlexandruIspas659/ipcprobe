// Package mhed implements the MHED discovery/configuration protocol used by TVT-made IP cameras (and OEM rebrands
// such as DVC) — the wire protocol behind the Windows "IPTool" / "IPC Manager" utility.
//
// This package is pure: it turns bytes into structs and back, with no sockets. See PROTOCOL.md for the wire format
// and testdata/vectors.json for the cross-language conformance vectors.
package mhed

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
)

// Transport constants.
const (
	Port    = 23456
	GroupTX = "234.55.55.55" // tool -> devices (search, set-network)
	GroupRX = "234.55.55.56" // devices -> tool (announce, ack)
)

// Commands (uint16 LE at offset 8).
const (
	CmdSearch   = 0x0001
	CmdAnnounce = 0x0002
	CmdSetNet   = 0x0003
	CmdSetAck   = 0x0010
)

// Header words.
const (
	verTX  = 0x0009 // tool -> device
	verRX  = 0x0008 // device -> tool
	hdrOne = 0x0001
)

// Packet lengths.
const (
	lenTX       = 140 // search, set-network
	lenAnnounce = 240
)

var magic = []byte("MHED")

// Announce field offsets (see PROTOCOL.md §cmd 2).
const (
	offName     = 12
	lenName     = 20
	offMAC      = 32
	offIP       = 40
	offMask     = 44
	offGateway  = 48
	offBuild    = 56
	offHTTP     = 60
	offRTSP     = 62
	offDNS1     = 112
	offDNS2     = 116
	offSerial   = 140
	offFirmware = 156
	offModel    = 196
	offVendor   = 212
	lenVendor   = 16
)

// Set-network field offsets (see PROTOCOL.md §cmd 3).
const (
	setMAC      = 32
	setIP       = 40
	setMask     = 44
	setGateway  = 48
	setPassword = 84
	lenPassword = 28
	setDNS1     = 112
	setDNS2     = 116
)

// Set-ack field offsets (see PROTOCOL.md §cmd 0x10).
const (
	ackIP   = 12
	ackPort = 16
)

// MaxPasswordBytes is the longest admin password that fits the 28-byte base64 field.
const MaxPasswordBytes = lenPassword / 4 * 3 // 21

// Device is a parsed cmd-2 announcement.
type Device struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Mask     string `json:"mask"`
	Gateway  string `json:"gateway"`
	DNS1     string `json:"dns1"`
	DNS2     string `json:"dns2"`
	Name     string `json:"name"`
	Serial   string `json:"serial"`
	Firmware string `json:"firmware"`
	Model    string `json:"model"`
	Vendor   string `json:"vendor"`
	HTTP     uint16 `json:"http"`
	RTSP     uint16 `json:"rtsp"`
	Build    string `json:"build"`
	Src      string `json:"src,omitempty"` // source address of the announcement (filled by the discovery layer)
}

// Command returns the command word of an MHED datagram, and false if it isn't one.
func Command(pkt []byte) (uint16, bool) {
	if len(pkt) < 10 || string(pkt[0:4]) != "MHED" {
		return 0, false
	}
	return binary.LittleEndian.Uint16(pkt[8:]), true
}

func header(cmd uint16) []byte {
	b := make([]byte, 10)
	copy(b, magic)
	binary.LittleEndian.PutUint16(b[4:], verTX)
	binary.LittleEndian.PutUint16(b[6:], hdrOne)
	binary.LittleEndian.PutUint16(b[8:], cmd)
	return b
}

// BuildSearch returns the 140-byte cmd-1 search probe.
func BuildSearch() []byte {
	pkt := make([]byte, lenTX)
	copy(pkt, header(CmdSearch))
	return pkt
}

// EncodePassword returns the base64 of pw, NUL-padded to 28 bytes. Error if the base64 form exceeds the field.
func EncodePassword(pw string) ([]byte, error) {
	enc := base64.StdEncoding.EncodeToString([]byte(pw))
	if len(enc) > lenPassword {
		return nil, fmt.Errorf("password too long: base64 form is %d bytes, the packet field holds %d (max %d raw bytes)",
			len(enc), lenPassword, MaxPasswordBytes)
	}
	field := make([]byte, lenPassword)
	copy(field, enc)
	return field, nil
}

// BuildSetNetwork returns the 140-byte cmd-3 set-network packet.
func BuildSetNetwork(mac net.HardwareAddr, ip, mask, gw, dns1, dns2 netip.Addr, password string) ([]byte, error) {
	if len(mac) != 6 {
		return nil, fmt.Errorf("MAC must be 6 bytes, got %d", len(mac))
	}
	for _, a := range []netip.Addr{ip, mask, gw, dns1, dns2} {
		if !a.Is4() {
			return nil, fmt.Errorf("addresses must be IPv4: %v", a)
		}
	}
	pw, err := EncodePassword(password)
	if err != nil {
		return nil, err
	}
	pkt := make([]byte, lenTX)
	copy(pkt, header(CmdSetNet))
	copy(pkt[setMAC:], mac)
	put4(pkt[setIP:], ip)
	put4(pkt[setMask:], mask)
	put4(pkt[setGateway:], gw)
	copy(pkt[setPassword:setPassword+lenPassword], pw)
	put4(pkt[setDNS1:], dns1)
	put4(pkt[setDNS2:], dns2)
	return pkt, nil
}

// ParseAnnounce parses a cmd-2 announcement, or returns false if the datagram isn't one.
func ParseAnnounce(pkt []byte) (*Device, bool) {
	if cmd, ok := Command(pkt); !ok || cmd != CmdAnnounce || len(pkt) < lenAnnounce {
		return nil, false
	}
	return &Device{
		MAC:      formatMAC(pkt[offMAC : offMAC+6]),
		IP:       ip4(pkt, offIP),
		Mask:     ip4(pkt, offMask),
		Gateway:  ip4(pkt, offGateway),
		DNS1:     ip4(pkt, offDNS1),
		DNS2:     ip4(pkt, offDNS2),
		Name:     cstr(pkt, offName, lenName),
		Serial:   cstr(pkt, offSerial, 16),
		Firmware: cstr(pkt, offFirmware, 16),
		Model:    cstr(pkt, offModel, 16),
		Vendor:   cstr(pkt, offVendor, lenVendor),
		HTTP:     binary.LittleEndian.Uint16(pkt[offHTTP:]),
		RTSP:     binary.LittleEndian.Uint16(pkt[offRTSP:]),
		Build:    buildDate(pkt),
	}, true
}

// ParseSetAck parses a cmd-0x10 ack, returning the echoed requester IP and port, or false.
func ParseSetAck(pkt []byte) (ip string, port uint16, ok bool) {
	if cmd, cok := Command(pkt); !cok || cmd != CmdSetAck || len(pkt) < ackPort+2 {
		return "", 0, false
	}
	return ip4(pkt, ackIP), binary.LittleEndian.Uint16(pkt[ackPort:]), true
}

// --- helpers ---

func put4(dst []byte, a netip.Addr) {
	b := a.As4()
	copy(dst[:4], b[:])
}

func ip4(pkt []byte, off int) string {
	a, _ := netip.AddrFromSlice(pkt[off : off+4])
	return a.String()
}

func cstr(pkt []byte, off, n int) string {
	b := pkt[off : off+n]
	if i := indexZero(b); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

func indexZero(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}

func formatMAC(b []byte) string {
	return net.HardwareAddr(b).String()
}

func buildDate(pkt []byte) string {
	v := binary.LittleEndian.Uint32(pkt[offBuild:])
	y, m, d := v/10000, (v/100)%100, v%100
	if y >= 2000 && y <= 2099 && m >= 1 && m <= 12 && d >= 1 && d <= 31 {
		return fmt.Sprintf("%04d-%02d-%02d", y, m, d)
	}
	return ""
}
