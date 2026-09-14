.PHONY: build test test-short fakecam mhedpcap snapshot clean

build:
	go build -o ipcprobe ./cmd/ipcprobe

test:
	go test ./...

# unit + vectors only (skips the end-to-end multicast test)
test-short:
	go test -short ./...

fakecam:
	go build -o fakecam ./cmd/fakecam

mhedpcap:
	go build -o mhedpcap ./cmd/mhedpcap

# local multi-platform build without publishing (needs goreleaser installed)
snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf dist ipcprobe ipcprobe-* fakecam mhedpcap
