// Package xlsx writes and reads the one kind of Excel workbook this tool needs: a single sheet that is a plain
// table of text, with a header row.
//
// A CSV file is the lowest common denominator, and Excel treats it as such: no column widths, no header, and —
// worse — it guesses at the cells. Where the full stop is the thousands separator it will happily turn the address
// 192.168.100.200 into the number 192168100200. An .xlsx file says what each cell is, so none of that happens:
// every cell here is stored as text and formatted as text, including the empty ones meant to be typed into.
//
// The format is a zip archive of XML files (ECMA-376). Only the standard library is used. Write produces the
// minimum Excel, Numbers and LibreOffice accept; Read accepts what those programs save back, which is why it
// understands shared strings and numeric cells although Write never produces either.
package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Sheet is a table to write: Rows[0] is the header.
type Sheet struct {
	Name string // the tab's name; "Sheet1" if empty
	Rows [][]string
	// Input names the header cells of the columns that are meant to be filled in. Those columns are tinted, so
	// that whoever opens the file sees where to type.
	Input []string
}

// Cell styles, as indexes into cellXfs in styles.xml below.
const (
	styleText   = 1 // text
	styleHeader = 2 // bold on grey
	styleInput  = 3 // text on a pale tint: type here
	styleInHead = 4 // header of an input column
)

// Write writes the sheet as an .xlsx workbook.
func Write(w io.Writer, s Sheet) error {
	name := s.Name
	if name == "" {
		name = "Sheet1"
	}
	name = sanitizeSheetName(name)
	cols := 0
	for _, r := range s.Rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	input := make([]bool, cols)
	if len(s.Rows) > 0 {
		for i, h := range s.Rows[0] {
			for _, want := range s.Input {
				if h == want {
					input[i] = true
				}
			}
		}
	}

	var sheet strings.Builder
	sheet.WriteString(xml.Header)
	sheet.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	// The header row stays in view while the cameras scroll.
	sheet.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
	sheet.WriteString(`<sheetFormatPr defaultRowHeight="16"/>`)
	if cols > 0 {
		sheet.WriteString(`<cols>`)
		for c := 0; c < cols; c++ {
			width := 6 // in characters; wide enough for the longest value, within reason
			for _, r := range s.Rows {
				if c < len(r) {
					if n := utf8.RuneCountInString(r[c]); n > width {
						width = n
					}
				}
			}
			if input[c] && width < 16 {
				width = 16 // room to type an address
			}
			if width > 60 {
				width = 60
			}
			style := styleText
			if input[c] {
				style = styleInput
			}
			// The column's own style is what a cell typed into below the table gets: text, not a guess.
			fmt.Fprintf(&sheet, `<col min="%d" max="%d" width="%d" style="%d" customWidth="1"/>`, c+1, c+1, width+3, style)
		}
		sheet.WriteString(`</cols>`)
	}
	sheet.WriteString(`<sheetData>`)
	for r, row := range s.Rows {
		fmt.Fprintf(&sheet, `<row r="%d">`, r+1)
		for c := 0; c < cols; c++ {
			v := ""
			if c < len(row) {
				v = row[c]
			}
			style := styleText
			switch {
			case r == 0 && input[c]:
				style = styleInHead
			case r == 0:
				style = styleHeader
			case input[c]:
				style = styleInput
			}
			ref := columnName(c) + strconv.Itoa(r+1)
			if v == "" {
				// An empty cell still carries its style: typed into, it stays text.
				fmt.Fprintf(&sheet, `<c r="%s" s="%d"/>`, ref, style)
				continue
			}
			fmt.Fprintf(&sheet, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">`, ref, style)
			if err := xml.EscapeText(&sheet, []byte(cleanText(v))); err != nil {
				return err
			}
			sheet.WriteString(`</t></is></c>`)
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData>`)
	if len(s.Rows) > 1 && cols > 0 {
		fmt.Fprintf(&sheet, `<autoFilter ref="A1:%s%d"/>`, columnName(cols-1), len(s.Rows))
	}
	sheet.WriteString(`</worksheet>`)

	var nameXML strings.Builder
	if err := xml.EscapeText(&nameXML, []byte(name)); err != nil {
		return err
	}
	// Excel records a sheet's filter range a second time, as a hidden name in the workbook; write it the way Excel
	// does rather than find out which readers mind its absence.
	filterName := ""
	if len(s.Rows) > 1 && cols > 0 {
		var quoted strings.Builder
		if err := xml.EscapeText(&quoted, []byte("'"+strings.ReplaceAll(name, "'", "''")+"'")); err != nil {
			return err
		}
		filterName = fmt.Sprintf(`<definedNames><definedName name="_xlnm._FilterDatabase" localSheetId="0" hidden="1">%s!$A$1:$%s$%d</definedName></definedNames>`,
			quoted.String(), columnName(cols-1), len(s.Rows))
	}
	files := []struct{ name, body string }{
		{"[Content_Types].xml", xml.Header + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
			`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
			`</Types>`},
		{"_rels/.rels", xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
			`</Relationships>`},
		{"xl/workbook.xml", xml.Header + `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
			`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<bookViews><workbookView/></bookViews>` +
			`<sheets><sheet name="` + nameXML.String() + `" sheetId="1" r:id="rId1"/></sheets>` + filterName + `</workbook>`},
		{"xl/_rels/workbook.xml.rels", xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
			`</Relationships>`},
		{"xl/styles.xml", stylesXML},
		{"xl/worksheets/sheet1.xml", sheet.String()},
	}
	zw := zip.NewWriter(w)
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(fw, f.body); err != nil {
			return err
		}
	}
	return zw.Close()
}

// numFmtId 49 is the built-in "Text" format ("@"): the cell is shown exactly as typed and never reinterpreted.
const stylesXML = xml.Header + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<fonts count="2"><font><sz val="12"/><name val="Calibri"/></font><font><b/><sz val="12"/><name val="Calibri"/></font></fonts>` +
	`<fills count="4"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>` +
	`<fill><patternFill patternType="solid"><fgColor rgb="FFE7E9EC"/><bgColor indexed="64"/></patternFill></fill>` +
	`<fill><patternFill patternType="solid"><fgColor rgb="FFFFF6D6"/><bgColor indexed="64"/></patternFill></fill></fills>` +
	`<borders count="2"><border><left/><right/><top/><bottom/><diagonal/></border>` +
	`<border><left/><right/><top/><bottom style="thin"><color rgb="FFB5BAC1"/></bottom><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="5">` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
	`<xf numFmtId="49" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="49" fontId="1" fillId="2" borderId="1" xfId="0" applyNumberFormat="1" applyFont="1" applyFill="1" applyBorder="1"/>` +
	`<xf numFmtId="49" fontId="0" fillId="3" borderId="0" xfId="0" applyNumberFormat="1" applyFill="1"/>` +
	`<xf numFmtId="49" fontId="1" fillId="3" borderId="1" xfId="0" applyNumberFormat="1" applyFont="1" applyFill="1" applyBorder="1"/>` +
	`</cellXfs>` +
	`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
	`</styleSheet>`

// columnName turns a 0-based column index into its letters: 0 -> A, 25 -> Z, 26 -> AA.
func columnName(i int) string {
	name := ""
	for i >= 0 {
		name = string(rune('A'+i%26)) + name
		i = i/26 - 1
	}
	return name
}

// columnIndex is the inverse, reading the letters at the start of a cell reference such as "AB12"; -1 if there
// are none.
func columnIndex(ref string) int {
	n := 0
	for _, c := range ref {
		switch {
		case c >= 'A' && c <= 'Z':
			n = n*26 + int(c-'A') + 1
		case c >= 'a' && c <= 'z':
			n = n*26 + int(c-'a') + 1
		default:
			return n - 1
		}
	}
	return n - 1
}

// cleanText drops the control characters XML 1.0 cannot carry (a NUL in a device name, say).
func cleanText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r != 0xFFFE && r != 0xFFFF) {
			return r
		}
		return -1
	}, s)
}

// sanitizeSheetName makes a name Excel accepts for a tab: at most 31 characters, none of \ / ? * [ ] :
func sanitizeSheetName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/?*[]:`, r) {
			return '-'
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 31 {
		s = string(r[:31])
	}
	return s
}

// IsWorkbook reports whether data looks like an .xlsx file (a zip archive).
func IsWorkbook(data []byte) bool { return bytes.HasPrefix(data, []byte("PK\x03\x04")) }

// Read returns the cells of the first sheet of an .xlsx workbook as text, row by row. Rows and cells that the
// file leaves out are returned empty, so a cell's position in the result is its position on the sheet.
func Read(data []byte) ([][]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not an .xlsx workbook: %v", err)
	}
	open := func(name string) ([]byte, error) {
		for _, f := range zr.File {
			if strings.EqualFold(f.Name, name) {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(io.LimitReader(rc, 64<<20)) // a plan is small; refuse to inflate a bomb
			}
		}
		return nil, errNoPart
	}

	sheetPath, err := firstSheetPath(open)
	if err != nil {
		return nil, err
	}
	sheet, err := open(sheetPath)
	if err != nil {
		return nil, fmt.Errorf("not an .xlsx workbook: %s is missing", sheetPath)
	}
	var shared []string
	if sst, err := open("xl/sharedStrings.xml"); err == nil {
		if shared, err = readSharedStrings(sst); err != nil {
			return nil, err
		}
	}

	var doc struct {
		Rows []struct {
			R     int `xml:"r,attr"`
			Cells []struct {
				Ref    string  `xml:"r,attr"`
				Type   string  `xml:"t,attr"`
				V      string  `xml:"v"`
				Inline richStr `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := xml.Unmarshal(sheet, &doc); err != nil {
		return nil, fmt.Errorf("unreadable sheet: %v", err)
	}
	var rows [][]string
	for i, r := range doc.Rows {
		at := r.R - 1
		if r.R == 0 {
			at = i // the row number is optional; without it rows simply follow each other
		}
		for len(rows) <= at {
			rows = append(rows, nil)
		}
		for j, c := range r.Cells {
			col := columnIndex(c.Ref)
			if col < 0 {
				col = j
			}
			var v string
			switch c.Type {
			case "s":
				if n, err := strconv.Atoi(strings.TrimSpace(c.V)); err == nil && n >= 0 && n < len(shared) {
					v = shared[n]
				}
			case "inlineStr":
				v = c.Inline.text()
			default: // a number, a formula's result, a boolean: whatever is stored
				v = c.V
			}
			for len(rows[at]) <= col {
				rows[at] = append(rows[at], "")
			}
			rows[at][col] = v
		}
	}
	return rows, nil
}

var errNoPart = errors.New("part not found")

// firstSheetPath finds the first sheet through the workbook's relationships, as the format intends; files written
// by the usual programs keep it at xl/worksheets/sheet1.xml, which is the fallback.
func firstSheetPath(open func(string) ([]byte, error)) (string, error) {
	const fallback = "xl/worksheets/sheet1.xml"
	wb, err := open("xl/workbook.xml")
	if err != nil {
		return "", fmt.Errorf("not an .xlsx workbook: xl/workbook.xml is missing")
	}
	var book struct {
		Sheets []struct {
			RID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(wb, &book); err != nil || len(book.Sheets) == 0 {
		return fallback, nil
	}
	relsData, err := open("xl/_rels/workbook.xml.rels")
	if err != nil {
		return fallback, nil
	}
	var rels struct {
		Rel []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(relsData, &rels); err != nil {
		return fallback, nil
	}
	for _, r := range rels.Rel {
		if r.ID == book.Sheets[0].RID && r.Target != "" {
			if strings.HasPrefix(r.Target, "/") {
				return strings.TrimPrefix(r.Target, "/"), nil
			}
			return path.Join("xl", r.Target), nil
		}
	}
	return fallback, nil
}

// richStr is a string as the format stores it: plain (<t>) or as formatted runs (<r><t>…). The phonetic guide
// some languages attach (<rPh>) is not part of the text.
type richStr struct {
	T    *string `xml:"t"`
	Runs []struct {
		T string `xml:"t"`
	} `xml:"r"`
}

func (s richStr) text() string {
	var b strings.Builder
	if s.T != nil {
		b.WriteString(*s.T)
	}
	for _, r := range s.Runs {
		b.WriteString(r.T)
	}
	return b.String()
}

func readSharedStrings(data []byte) ([]string, error) {
	var sst struct {
		Items []richStr `xml:"si"`
	}
	if err := xml.Unmarshal(data, &sst); err != nil {
		return nil, fmt.Errorf("unreadable shared strings: %v", err)
	}
	out := make([]string, len(sst.Items))
	for i, it := range sst.Items {
		out[i] = it.text()
	}
	return out, nil
}
