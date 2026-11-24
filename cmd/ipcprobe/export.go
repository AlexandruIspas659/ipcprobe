package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/netip"

	"github.com/AlexandruIspas659/ipcprobe/internal/batch"
	"github.com/AlexandruIspas659/ipcprobe/internal/mhed"
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

func writeCSV(w io.Writer, rows [][]string) error {
	cw := csv.NewWriter(w)
	if err := cw.WriteAll(rows); err != nil { // WriteAll flushes
		return err
	}
	return cw.Error()
}
