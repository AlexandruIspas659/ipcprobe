package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strings"

	"github.com/AlexandruIspas659/ipcprobe/internal/batch"
	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
	"github.com/AlexandruIspas659/ipcprobe/internal/xlsx"
)

// CSV output is for people: something to open in a spreadsheet, hand to whoever configures the recorder, or file
// with the site's documentation. Scripts should use --json, whose keys are a contract; these headers are not.

// deviceRows is the device list as a header row plus one row per device.
func deviceRows(devs []*mhed.Device) [][]string {
	notes := deviceNotes(devs)
	rows := [][]string{{"MAC", "IP", "Mask", "Gateway", "DNS 1", "DNS 2", "Name", "Model", "Firmware", "Build",
		"Serial", "Vendor", "HTTP", "RTSP", "Web URL", "RTSP URL", "Note"}}
	for _, d := range devs {
		rows = append(rows, []string{d.MAC, d.IP, d.Mask, d.Gateway, d.DNS1, d.DNS2, d.Name, d.Model, d.Firmware,
			d.Build, d.Serial, d.Vendor, fmt.Sprint(d.HTTP), fmt.Sprint(d.RTSP), webURL(d), rtspURL(d), notes[d.MAC]})
	}
	return rows
}

// webURL and rtspURL are where the camera's web page and video stream are — what a recorder or a VMS asks for when
// a camera is added by hand. Empty when the camera announces no such port.
func webURL(d *mhed.Device) string {
	if d.HTTP == 0 {
		return ""
	}
	return "http://" + d.IP + portSuffix(d.HTTP, 80) + "/"
}

func rtspURL(d *mhed.Device) string {
	if d.RTSP == 0 {
		return ""
	}
	return "rtsp://" + d.IP + portSuffix(d.RTSP, 554) + "/"
}

// templateRows is a plan waiting to be filled in: the cameras that are there, each with its MAC, its name and model
// to recognise it by, and where it is now — followed by empty New IP / Mask / Gateway / DNS columns. Type into
// those, leave the rest blank (blank means keep), and the file is a plan for `apply --plan`. Handed back untouched
// it changes nothing.
func templateRows(devs []*mhed.Device) [][]string {
	rows := [][]string{{"MAC", "Name", "Model", "IP", "New IP", "Mask", "Gateway", "DNS 1", "DNS 2"}}
	for _, d := range devs {
		rows = append(rows, []string{d.MAC, d.Name, d.Model, d.IP, "", "", "", "", ""})
	}
	return rows
}

// batchRows is a batch plan or outcome as a header row plus one row per camera: where it was, where it is meant to
// be, and how that went. The network columns hold the target configuration (the current one where there is no
// target), so after a successful run the sheet reads as the cameras' new settings.
func batchRows(items []batch.Item) [][]string {
	rows := [][]string{{"MAC", "Name", "Model", "Old IP", "New IP", "Mask", "Gateway", "DNS 1", "DNS 2", "Status", "Note"}}
	str := func(a netip.Addr) string {
		if !a.IsValid() {
			return ""
		}
		return a.String()
	}
	for i := range items {
		it := &items[i]
		var oldIP string
		var cfg batch.Config
		if it.Current != nil {
			oldIP, cfg = str(it.Current.IP), *it.Current
		}
		if it.Target != nil {
			cfg = *it.Target
		}
		note := it.Error
		for _, w := range it.Warnings {
			if note != "" {
				note += "; "
			}
			note += "warning: " + w
		}
		rows = append(rows, []string{it.MAC, it.Name, it.Model, oldIP, str(cfg.IP), str(cfg.Mask), str(cfg.Gateway),
			str(cfg.DNS1), str(cfg.DNS2), string(it.Status), note})
	}
	return rows
}

// planInputColumns are the columns of a plan that are there to be typed into; a workbook tints them.
var planInputColumns = []string{"New IP", "Mask", "Gateway", "DNS 1", "DNS 2"}

// writeWorkbook saves rows as an Excel workbook. Unlike a CSV file it opens the way it was meant: columns as wide
// as their contents, the header set apart and frozen, and every cell text — Excel does not get to decide that an
// address is a number.
func writeWorkbook(path, sheet string, rows [][]string, input []string) error {
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, xlsx.Sheet{Name: sheet, Rows: rows, Input: input}); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// cmdXLSX writes a table handed over as JSON on stdin — {"sheet": "...", "rows": [[...], ...], "input": [...]} —
// as a workbook. It exists for the macOS app, which knows what is on screen (the filtered list, the selection, a
// batch's results) but leaves the writing of files in this format to the helper, like everything else it could
// get wrong on its own.
func cmdXLSX(args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "--") {
		fmt.Fprintln(os.Stderr, "usage: ipcprobe xlsx <file.xlsx>   (the table is read as JSON from stdin)")
		return 2
	}
	var in struct {
		Sheet string     `json:"sheet"`
		Rows  [][]string `json:"rows"`
		Input []string   `json:"input"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fmt.Fprintln(os.Stderr, "error: expected {\"sheet\", \"rows\", \"input\"} as JSON on stdin:", err)
		return 2
	}
	if err := writeWorkbook(args[0], in.Sheet, in.Rows, in.Input); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func writeCSV(w io.Writer, rows [][]string) error {
	cw := csv.NewWriter(w)
	if err := cw.WriteAll(rows); err != nil { // WriteAll flushes
		return err
	}
	return cw.Error()
}
