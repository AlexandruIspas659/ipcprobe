package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/AlexandruIspas659/ipcprobe/internal/batch"
	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
	"github.com/AlexandruIspas659/ipcprobe/internal/xlsx"
)

func describePlan(reqs []batch.Request) string {
	var parts []string
	for _, r := range reqs {
		s := r.MAC[15:]
		for _, f := range []struct {
			name string
			ok   bool
			v    string
		}{{"ip", r.IP.IsValid(), r.IP.String()}, {"mask", r.Mask.IsValid(), r.Mask.String()}, {"gw", r.Gateway.IsValid(), r.Gateway.String()},
			{"dns1", r.DNS1.IsValid(), r.DNS1.String()}, {"dns2", r.DNS2.IsValid(), r.DNS2.String()}} {
			if f.ok {
				s += " " + f.name + "=" + f.v
			}
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " | ")
}

func TestCSVPlan(t *testing.T) {
	const want = "01 ip=192.168.1.100 | 02 ip=192.168.1.101 gw=192.168.1.254"
	for name, text := range map[string]string{
		"commas":     "MAC,New IP,Gateway\n02:00:00:00:00:01,192.168.1.100,\n02:00:00:00:00:02,192.168.1.101,192.168.1.254\n",
		"semicolons": "MAC;New IP;Gateway\r\n02:00:00:00:00:01;192.168.1.100;\r\n02:00:00:00:00:02;192.168.1.101;192.168.1.254\r\n",
		"tabs":       "MAC\tNew IP\tGateway\n02:00:00:00:00:01\t192.168.1.100\t\n02:00:00:00:00:02\t192.168.1.101\t192.168.1.254\n",
		"excel":      "\xEF\xBB\xBFMAC;New IP;Gateway\r\n02:00:00:00:00:01;192.168.1.100\r\n\r\n;;\r\n02-00-00-00-00-02; 192.168.1.101 ;192.168.1.254\r\n",
		"any header": "name,gw,Mac Address,ip\nlobby,,02:00:00:00:00:01,192.168.1.100\ngate,192.168.1.254,02:00:00:00:00:02,192.168.1.101\n",
	} {
		reqs, err := parsePlan([]byte(text))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got := describePlan(reqs); got != want {
			t.Errorf("%s: %s", name, got)
		}
	}
}

// The files the tool itself writes have to come back in: the camera list with addresses typed over, and the
// outcome of a batch, which has both an old and a new address.
func TestOwnExportsAreValidPlans(t *testing.T) {
	devs := []*mhed.Device{
		{MAC: "02:00:00:00:00:01", IP: "192.168.1.88", Mask: "255.255.255.0", Gateway: "192.168.1.1", DNS1: "192.168.1.1", DNS2: "8.8.8.8", Name: `lobby, "north"`},
		{MAC: "02:00:00:00:00:02", IP: "192.168.1.88", Mask: "255.255.255.0", Gateway: "192.168.1.1", DNS1: "192.168.1.1", DNS2: "8.8.8.8"},
	}
	var list bytes.Buffer
	if err := writeCSV(&list, deviceRows(devs)); err != nil {
		t.Fatal(err)
	}
	reqs, err := parsePlan(list.Bytes())
	if err != nil {
		t.Fatalf("camera list export: %v", err)
	}
	if got := describePlan(reqs[:1]); got != "01 ip=192.168.1.88 mask=255.255.255.0 gw=192.168.1.1 dns1=192.168.1.1 dns2=8.8.8.8" {
		t.Errorf("camera list export: %s", got)
	}

	items := batch.Build(map[string]*mhed.Device{devs[0].MAC: devs[0], devs[1].MAC: devs[1]},
		[]batch.Request{{MAC: devs[0].MAC, Config: batch.Config{IP: reqs[0].IP.Next()}}})
	var outcome bytes.Buffer
	if err := writeCSV(&outcome, batchRows(items)); err != nil {
		t.Fatal(err)
	}
	reqs, err = parsePlan(outcome.Bytes())
	if err != nil {
		t.Fatalf("batch outcome export: %v", err)
	}
	if !strings.HasPrefix(describePlan(reqs), "01 ip=192.168.1.89 ") { // "New IP", not "Old IP"
		t.Errorf("batch outcome export: %s", describePlan(reqs))
	}
}

func TestCSVPlanNewIPWinsAndBlankMeansKeep(t *testing.T) {
	reqs, err := parsePlan([]byte("MAC,IP,New IP,Mask\n02:00:00:00:00:01,192.168.1.88,192.168.1.100,\n02:00:00:00:00:02,192.168.1.88,,255.255.0.0\n"))
	if err != nil {
		t.Fatal(err)
	}
	// The second camera has no new address: it keeps the one it has — the IP column is where it was, not an order.
	if got := describePlan(reqs); got != "01 ip=192.168.1.100 | 02 mask=255.255.0.0" {
		t.Error(got)
	}
}

func TestPlanErrors(t *testing.T) {
	for name, c := range map[string]struct{ text, want string }{
		"bad address":    {"MAC,New IP\n02:00:00:00:00:01,192.168.1.100\n02:00:00:00:00:02,192.168.1.300\n", `row 3 (02:00:00:00:00:02): ip "192.168.1.300"`},
		"bad MAC":        {"MAC,New IP\nnot-a-mac,192.168.1.100\n", "row 2: invalid MAC"},
		"no MAC":         {"MAC,New IP\n,192.168.1.100\n", "row 2 has settings but no MAC"},
		"no MAC column":  {"Name,New IP\nlobby,192.168.1.100\n", "no MAC column"},
		"nothing to do":  {"MAC,Name\n02:00:00:00:00:01,lobby\n", "nothing to apply"},
		"header only":    {"MAC,New IP\n", "the plan is empty"},
		"empty file":     {"", "the plan is empty"},
		"csv password":   {"MAC,New IP,Admin Password\n02:00:00:00:00:01,192.168.1.100,secret\n", "contains a password"},
		"json password":  {`[{"mac":"02:00:00:00:00:01","ip":"192.168.1.100","password":"secret"}]`, "contains a password"},
		"json malformed": {`[{"mac":`, "expected a JSON array"},
		"json bad addr":  {`[{"mac":"02:00:00:00:00:01","gateway":"nope"}]`, `entry 1 (02:00:00:00:00:01): gateway "nope"`},
	} {
		_, err := parsePlan([]byte(c.text))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want one containing %q", name, err, c.want)
		}
		if err != nil && strings.Contains(err.Error(), "secret") {
			t.Errorf("%s: the error repeats the password", name)
		}
	}
}

func TestJSONPlan(t *testing.T) {
	// `list --json` output, edited: unknown keys are ignored, empty strings mean keep.
	reqs, err := parsePlan([]byte(` [{"mac":"02:00:00:00:00:01","ip":"192.168.1.100","mask":"","name":"lobby","http":80,"foreign":false},
		{"mac":"02-00-00-00-00-02","dns1":"1.1.1.1"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if got := describePlan(reqs); got != "01 ip=192.168.1.100 | 02 dns1=1.1.1.1" {
		t.Error(got)
	}
}

// The example plan shipped in testdata must stay a valid plan: it is what the README tells people to try.
func TestExamplePlan(t *testing.T) {
	reqs, err := readPlan("../../testdata/example-plan.csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 6 {
		t.Fatalf("%d cameras, want 6", len(reqs))
	}
	if got := describePlan(reqs[2:5]); got != "03 ip=192.168.1.203 dns2=1.1.1.1 | 04 ip=192.168.1.204 | 05 dns2=9.9.9.9" {
		t.Error(got)
	}
}

// A template is a plan with nothing asked yet: handed back untouched it changes nothing, and each cell typed into
// it becomes exactly one request.
func TestTemplateIsAPlanToFillIn(t *testing.T) {
	devs := []*mhed.Device{
		{MAC: "02:00:00:00:00:01", IP: "192.168.1.88", Mask: "255.255.255.0", Gateway: "192.168.1.1", DNS1: "192.168.1.1", DNS2: "8.8.8.8", Name: "DVC"},
		{MAC: "02:00:00:00:00:02", IP: "192.168.1.88", Mask: "255.255.255.0", Gateway: "192.168.1.1", DNS1: "192.168.1.1", DNS2: "8.8.8.8", Name: "DVC"},
	}
	rows := templateRows(devs)
	var untouched bytes.Buffer
	if err := writeCSV(&untouched, rows); err != nil {
		t.Fatal(err)
	}
	reqs, err := parsePlan(untouched.Bytes())
	if err != nil {
		t.Fatalf("untouched template: %v", err)
	}
	// The IP column says where each camera is now; it must not be read as an instruction.
	if got := describePlan(reqs); got != "01 | 02" {
		t.Errorf("untouched template asks for something: %s", got)
	}
	items := batch.Build(map[string]*mhed.Device{devs[0].MAC: devs[0], devs[1].MAC: devs[1]}, reqs)
	if batch.Count(items, batch.Unchanged) != 2 {
		t.Errorf("untouched template would change cameras: %+v", items)
	}

	col := map[string]int{}
	for i, h := range rows[0] {
		col[h] = i
	}
	rows[1][col["New IP"]] = "192.168.1.101"
	rows[2][col["Gateway"]] = "192.168.1.254"
	var filled bytes.Buffer
	if err := writeCSV(&filled, rows); err != nil {
		t.Fatal(err)
	}
	if reqs, err = parsePlan(filled.Bytes()); err != nil {
		t.Fatal(err)
	}
	if got := describePlan(reqs); got != "01 ip=192.168.1.101 | 02 gw=192.168.1.254" {
		t.Errorf("filled template: %s", got)
	}
}

// A workbook is a plan like any other: the template saved as .xlsx, filled in, comes back as the same requests a
// CSV would give.
func TestWorkbookPlan(t *testing.T) {
	devs := []*mhed.Device{
		{MAC: "02:00:00:00:00:01", IP: "192.168.100.200", Name: "DVC"},
		{MAC: "02:00:00:00:00:02", IP: "192.168.100.200", Name: "DVC"},
	}
	rows := templateRows(devs)
	rows[1][4] = "192.168.100.201" // New IP
	rows[2][6] = "192.168.100.254" // Gateway
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, xlsx.Sheet{Name: "Plan", Rows: rows, Input: planInputColumns}); err != nil {
		t.Fatal(err)
	}
	reqs, err := parsePlan(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got := describePlan(reqs); got != "01 ip=192.168.100.201 | 02 gw=192.168.100.254" {
		t.Error(got)
	}

	// The same refusals apply.
	buf.Reset()
	xlsx.Write(&buf, xlsx.Sheet{Rows: [][]string{{"MAC", "New IP", "Password"}, {"02:00:00:00:00:01", "192.168.1.5", "secret"}}})
	if _, err := parsePlan(buf.Bytes()); err == nil || !strings.Contains(err.Error(), "contains a password") {
		t.Errorf("workbook with a password column: %v", err)
	}
	buf.Reset()
	xlsx.Write(&buf, xlsx.Sheet{Rows: [][]string{{"MAC", "New IP"}, {"02:00:00:00:00:01", "192168100201"}}})
	if _, err := parsePlan(buf.Bytes()); err == nil || !strings.Contains(err.Error(), "row 2") {
		t.Errorf("an address Excel turned into a number must be reported, not applied: %v", err)
	}
}
