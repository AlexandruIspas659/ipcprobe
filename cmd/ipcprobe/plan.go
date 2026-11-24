package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"strings"

	"github.com/AlexandruIspas659/ipcprobe/internal/batch"
)

// A plan file says, camera by camera, what to change: a MAC and any of IP / mask / gateway / DNS 1 / DNS 2, with
// whatever is left out kept from the camera's current configuration. Two forms are read:
//
//   - JSON: an array of {"mac","ip","mask","gateway","dns1","dns2"} objects — the shape of `list --json`.
//   - A spreadsheet saved as CSV: a header row, then one row per camera. This is the form for people: export the
//     camera list (`list --csv`, or the app), type the new addresses into it, and feed it back.
//
// A plan never contains a password; a file that has one is refused rather than read.

// readPlan reads a plan file in either form.
func readPlan(path string) ([]batch.Request, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	reqs, err := parsePlan(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return reqs, nil
}

// parsePlan decides between the two forms by the first character: a JSON plan starts with "[".
func parsePlan(data []byte) ([]batch.Request, error) {
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF")) // the byte-order mark Excel puts in front of UTF-8
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] == '[' {
		return parseJSONPlan(trimmed)
	}
	return parseCSVPlan(data)
}

// planEntry is one camera of a plan before its addresses are parsed; where is "entry 3" or "row 4".
type planEntry struct {
	where                         string
	mac, ip, mask, gw, dns1, dns2 string
}

func parseJSONPlan(data []byte) ([]batch.Request, error) {
	var raw []struct {
		MAC      string `json:"mac"`
		IP       string `json:"ip"`
		Mask     string `json:"mask"`
		Gateway  string `json:"gateway"`
		DNS1     string `json:"dns1"`
		DNS2     string `json:"dns2"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("expected a JSON array of {\"mac\", \"ip\", \"mask\", \"gateway\", \"dns1\", \"dns2\"} objects: %v", err)
	}
	entries := make([]planEntry, 0, len(raw))
	for i, e := range raw {
		if e.Password != "" {
			return nil, errPlanPassword
		}
		entries = append(entries, planEntry{fmt.Sprintf("entry %d", i+1), e.MAC, e.IP, e.Mask, e.Gateway, e.DNS1, e.DNS2})
	}
	return planRequests(entries)
}

var errPlanPassword = fmt.Errorf("the plan contains a password: remove it — the password is asked for when the plan is applied and is never read from a file")

// parseCSVPlan reads a spreadsheet saved as CSV. The separator is whichever of comma, semicolon and tab the header
// row uses most — Excel writes semicolons wherever the comma is the decimal sign — and columns are found by their
// header, in any order and any capitalisation:
//
//	MAC                  required
//	New IP, else IP      the address to give the camera ("New IP" wins when both are there, so the export of a
//	                     finished batch, which has "Old IP" and "New IP", can be fed back as it is)
//	Mask, Gateway, DNS 1, DNS 2
//
// Every other column is ignored, an empty cell means "keep", and empty rows are skipped.
func parseCSVPlan(data []byte) ([]batch.Request, error) {
	firstLine := data
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		firstLine = data[:i]
	}
	sep := ','
	for _, c := range []rune{';', '\t'} {
		if bytes.Count(firstLine, []byte(string(c))) > bytes.Count(firstLine, []byte(string(sep))) {
			sep = c
		}
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = sep
	r.FieldsPerRecord = -1 // spreadsheets drop trailing empty cells
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("not a readable CSV file: %v", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("the plan is empty")
	}

	col := map[string]int{}
	for i, h := range rows[0] {
		key := strings.Map(func(c rune) rune { // "DNS 1", "dns_1", "Dns-1" -> "dns1"
			if c == ' ' || c == '_' || c == '-' || c == '.' {
				return -1
			}
			return c
		}, strings.ToLower(strings.TrimSpace(h)))
		if strings.Contains(key, "password") {
			return nil, errPlanPassword
		}
		if _, dup := col[key]; !dup {
			col[key] = i
		}
	}
	find := func(names ...string) int {
		for _, n := range names {
			if i, ok := col[n]; ok {
				return i
			}
		}
		return -1
	}
	macCol := find("mac", "macaddress")
	ipCol := find("newip", "newaddress", "ip", "ipaddress", "address")
	maskCol, gwCol := find("mask", "subnetmask", "netmask"), find("gateway", "gw")
	dns1Col, dns2Col := find("dns1"), find("dns2")
	if macCol < 0 {
		return nil, fmt.Errorf("no MAC column: the first row has to name the columns (MAC, New IP or IP, Mask, Gateway, DNS 1, DNS 2)")
	}
	if ipCol < 0 && maskCol < 0 && gwCol < 0 && dns1Col < 0 && dns2Col < 0 {
		return nil, fmt.Errorf("nothing to apply: besides MAC, expected at least one of the columns New IP (or IP), Mask, Gateway, DNS 1, DNS 2")
	}
	cell := func(row []string, i int) string {
		if i < 0 || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	var entries []planEntry
	for n, row := range rows[1:] {
		e := planEntry{fmt.Sprintf("row %d", n+2), // as a spreadsheet numbers it: the header is row 1
			cell(row, macCol), cell(row, ipCol), cell(row, maskCol), cell(row, gwCol), cell(row, dns1Col), cell(row, dns2Col)}
		if e.mac == "" {
			if e.ip+e.mask+e.gw+e.dns1+e.dns2 == "" {
				continue // an empty row, or one with notes in other columns
			}
			return nil, fmt.Errorf("%s has settings but no MAC", e.where)
		}
		entries = append(entries, e)
	}
	return planRequests(entries)
}

func planRequests(entries []planEntry) ([]batch.Request, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("the plan is empty")
	}
	reqs := make([]batch.Request, 0, len(entries))
	for _, e := range entries {
		hw, err := parseMAC(e.mac)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", e.where, err)
		}
		r := batch.Request{MAC: hw.String()}
		for _, o := range []struct {
			name, s string
			dst     *netip.Addr
		}{{"ip", e.ip, &r.IP}, {"mask", e.mask, &r.Mask}, {"gateway", e.gw, &r.Gateway}, {"dns1", e.dns1, &r.DNS1}, {"dns2", e.dns2, &r.DNS2}} {
			if o.s == "" {
				continue
			}
			a, err := netip.ParseAddr(o.s)
			if err != nil || !a.Is4() {
				return nil, fmt.Errorf("%s (%s): %s %q is not an IPv4 address", e.where, hw, o.name, o.s)
			}
			*o.dst = a
		}
		reqs = append(reqs, r)
	}
	return reqs, nil
}
