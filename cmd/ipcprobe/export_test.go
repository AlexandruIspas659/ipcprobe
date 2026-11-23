package main

import (
	"bytes"
	"encoding/csv"
	"net/netip"
	"strings"
	"testing"

	"github.com/AlexandruIspas659/ipcprobe/internal/batch"
	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
)

func TestDeviceRows(t *testing.T) {
	devs := []*mhed.Device{
		{MAC: "02:00:00:00:00:01", IP: "10.0.0.5", Mask: "255.0.0.0", Gateway: "10.0.0.1", DNS1: "10.0.0.1", DNS2: "8.8.8.8",
			Name: `lobby, "north"`, Model: "DCN-TEST", HTTP: 80, RTSP: 554, Foreign: true},
		{MAC: "02:00:00:00:00:02", IP: "10.0.0.5", Mask: "255.0.0.0", Gateway: "10.0.0.1", DNS1: "10.0.0.1", DNS2: "8.8.8.8",
			Name: "gate", Model: "DCN-TEST", HTTP: 8080},
	}
	rows := deviceRows(devs)
	if len(rows) != 3 || len(rows[0]) != len(rows[1]) || rows[0][0] != "MAC" {
		t.Fatalf("rows: %v", rows)
	}
	last := len(rows[0]) - 1
	if rows[1][last] != "other subnet, address shared by 2" || rows[2][last] != "address shared by 2" {
		t.Errorf("notes: %q / %q", rows[1][last], rows[2][last])
	}

	// A name with a comma and quotes must survive a round trip through CSV.
	var buf bytes.Buffer
	if err := writeCSV(&buf, rows); err != nil {
		t.Fatal(err)
	}
	back, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if back[1][6] != `lobby, "north"` || back[2][12] != "8080" {
		t.Errorf("round trip: name %q, http %q", back[1][6], back[2][12])
	}
}

func TestBatchRows(t *testing.T) {
	a := func(s string) netip.Addr { return netip.MustParseAddr(s) }
	cur := batch.Config{IP: a("192.168.1.88"), Mask: a("255.255.255.0"), Gateway: a("192.168.1.1"), DNS1: a("192.168.1.1"), DNS2: a("8.8.8.8")}
	tgt := cur
	tgt.IP = a("192.168.1.100")
	items := []batch.Item{
		{MAC: "02:00:00:00:00:01", Name: "lobby", Current: &cur, Target: &tgt, Status: batch.Confirmed,
			Warnings: []string{"gateway is a guess"}},
		{MAC: "02:00:00:00:00:02", Status: batch.NotSeen, Error: "not seen in discovery"},
	}
	rows := batchRows(items)
	if got := strings.Join(rows[1], "|"); got != "02:00:00:00:00:01|lobby||192.168.1.88|192.168.1.100|255.255.255.0|192.168.1.1|192.168.1.1|8.8.8.8|confirmed|warning: gateway is a guess" {
		t.Errorf("confirmed row: %s", got)
	}
	// A camera that was never seen has no addresses; the columns are empty, not "invalid IP".
	if got := strings.Join(rows[2], "|"); got != "02:00:00:00:00:02|||||||||not-seen|not seen in discovery" {
		t.Errorf("not-seen row: %s", got)
	}
}

func TestDescribeChange(t *testing.T) {
	old := mhed.Device{MAC: "02:00:00:00:00:01", IP: "192.168.1.88", Gateway: "192.168.1.1", Name: "DVC", HTTP: 80}
	cur := old
	cur.IP, cur.Gateway, cur.HTTP = "192.168.1.100", "192.168.1.254", 8080
	if got := describeChange(old, cur); got != "ip 192.168.1.88 -> 192.168.1.100, gateway 192.168.1.1 -> 192.168.1.254, http 80 -> 8080" {
		t.Errorf("describeChange: %q", got)
	}
	cur = old
	cur.Src = "192.168.1.99" // not a field anyone configures
	if got := describeChange(old, cur); got != "announcement changed" {
		t.Errorf("describeChange with only an unlisted difference: %q", got)
	}
}
