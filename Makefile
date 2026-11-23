.PHONY: build test test-short fakecam mhedpcap app run-app snapshot clean

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

# the macOS app: macos/dist/ipcprobe.app + .dmg (needs Xcode or the Command Line Tools)
app:
	macos/build-app.sh

# run the app straight from source against a freshly built helper — no bundle, for quick UI work.
# Works from any directory:  make -C path/to/ipcprobe run-app
run-app:
	go build -o macos/.build/ipcprobe-dev ./cmd/ipcprobe
	cd macos && IPCPROBE_BIN="$(CURDIR)/macos/.build/ipcprobe-dev" swift run IPCProbe

# local multi-platform build without publishing (needs goreleaser installed)
snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf dist ipcprobe ipcprobe-* fakecam mhedpcap macos/dist macos/.build
