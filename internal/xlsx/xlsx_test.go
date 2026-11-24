package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestWriteThenRead(t *testing.T) {
	rows := [][]string{
		{"MAC", "Name", "IP", "New IP", "Note"},
		{"02:00:00:00:00:01", `lobby & "north" <1>`, "192.168.100.200", "", "  leading spaces"},
		{"02:00:00:00:00:02", "", "10.0.0.5", "10.0.0.6", "ünïcödé"},
		{"02:00:00:00:00:03", "nul\x00inside"},
	}
	var buf bytes.Buffer
	if err := Write(&buf, Sheet{Name: "Cameras", Rows: rows, Input: []string{"New IP"}}); err != nil {
		t.Fatal(err)
	}
	if !IsWorkbook(buf.Bytes()) {
		t.Fatal("what Write wrote is not recognised as a workbook")
	}
	got, err := Read(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		rows[0], rows[1], rows[2],
		{"02:00:00:00:00:03", "nulinside", "", "", ""}, // short rows are padded; the NUL cannot be stored
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip:\n got %q\nwant %q", got, want)
	}
}

// Every part has to be well-formed XML, and the sheet has to say what makes it better than a CSV: text cells,
// a frozen header, the input column tinted even where it is empty.
func TestWrittenParts(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, Sheet{Name: "a/b:c*d?e[f]g" + strings.Repeat("x", 40), Rows: [][]string{{"MAC", "New IP"}, {"02:00:00:00:00:01", ""}}, Input: []string{"New IP"}})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		parts[f.Name] = string(b)
		d := xml.NewDecoder(bytes.NewReader(b))
		for {
			if _, err := d.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s is not well-formed XML: %v", f.Name, err)
			}
		}
	}
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml"} {
		if parts[name] == "" {
			t.Errorf("part %s is missing", name)
		}
	}
	sheet := parts["xl/worksheets/sheet1.xml"]
	for what, fragment := range map[string]string{
		"frozen header":                    `state="frozen"`,
		"header cell":                      `<c r="A1" s="2" t="inlineStr">`,
		"input column header":              `<c r="B1" s="4" t="inlineStr">`,
		"text cell":                        `<c r="A2" s="1" t="inlineStr">`,
		"empty input cell keeps its style": `<c r="B2" s="3"/>`,
		"input column is text below too":   `<col min="2" max="2" width="19" style="3"`,
		"filter on the header":             `<autoFilter ref="A1:B2"/>`,
	} {
		if !strings.Contains(sheet, fragment) {
			t.Errorf("%s: %s not found in the sheet", what, fragment)
		}
	}
	if !strings.Contains(parts["xl/styles.xml"], `numFmtId="49"`) {
		t.Error("no text number format in the styles")
	}
	if !strings.Contains(parts["xl/workbook.xml"], `name="a-b-c-d-e-f-gxxxxxxxxxxxxxxxxxx"`) { // 31 characters, none forbidden
		t.Errorf("sheet name not sanitised: %s", parts["xl/workbook.xml"])
	}
	if !strings.Contains(parts["xl/workbook.xml"], `hidden="1">&#39;a-b-c-d-e-f-gxxxxxxxxxxxxxxxxxx&#39;!$A$1:$B$2</definedName>`) {
		t.Errorf("filter range not recorded in the workbook: %s", parts["xl/workbook.xml"])
	}
}

// What Excel saves back looks different from what Write produces: strings live in a shared table (some as
// formatted runs), typed numbers are numeric cells, empty cells and rows are simply left out, and the sheet may
// not be called sheet1.xml.
func TestReadWhatExcelSaves(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, _ := zw.Create(name)
		io.WriteString(w, body)
	}
	add("xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Plan" sheetId="7" r:id="rId3"/><sheet name="Notes" sheetId="8" r:id="rId4"/></sheets></workbook>`)
	add("xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId4" Type="x" Target="worksheets/notes.xml"/><Relationship Id="rId3" Type="x" Target="worksheets/plan.xml"/></Relationships>`)
	add("xl/sharedStrings.xml", `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>MAC</t></si><si><t>New IP</t></si><si><t>02:00:00:00:00:01</t></si><si><r><rPr><b/></rPr><t>192.168.</t></r><r><t>1.201</t></r><rPh><t>ignored</t></rPh></si><si><t>HTTP</t></si></sst>`)
	add("xl/worksheets/notes.xml", `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>wrong sheet</t></is></c></row></sheetData></worksheet>`)
	add("xl/worksheets/plan.xml", `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`+
		`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="C1" t="s"><v>1</v></c><c r="D1" t="s"><v>4</v></c></row>`+
		`<row r="3"><c r="A3" t="s"><v>2</v></c><c r="C3" t="s"><v>3</v></c><c r="D3"><v>8080</v></c></row>`+
		`</sheetData></worksheet>`)
	zw.Close()

	got, err := Read(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"MAC", "", "New IP", "HTTP"}, nil, {"02:00:00:00:00:01", "", "192.168.1.201", "8080"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("\n got %q\nwant %q", got, want)
	}
}

func TestReadRejectsOtherFiles(t *testing.T) {
	if _, err := Read([]byte("MAC,New IP\n")); err == nil {
		t.Error("a CSV was read as a workbook")
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	io.WriteString(w, "<doc/>")
	zw.Close()
	if _, err := Read(buf.Bytes()); err == nil {
		t.Error("a zip that is not a workbook was read as one")
	}
	if IsWorkbook([]byte("MAC,New IP\n")) {
		t.Error("a CSV looks like a workbook")
	}
}

func TestColumnNames(t *testing.T) {
	for i, want := range map[int]string{0: "A", 25: "Z", 26: "AA", 27: "AB", 51: "AZ", 52: "BA", 701: "ZZ", 702: "AAA"} {
		if got := columnName(i); got != want {
			t.Errorf("columnName(%d) = %s, want %s", i, got, want)
		}
		if got := columnIndex(want + "12"); got != i {
			t.Errorf("columnIndex(%s12) = %d, want %d", want, got, i)
		}
	}
	if columnIndex("12") != -1 || columnIndex("") != -1 {
		t.Error("a reference without letters has no column")
	}
}
