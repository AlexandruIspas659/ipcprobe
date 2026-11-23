.PHONY: build test test-short test-app fakecam mhedpcap app run-app screens snapshot clean

build:
	go build -o ipcprobe ./cmd/ipcprobe

test:
	go test ./...

# unit + vectors only (skips the end-to-end multicast test)
test-short:
	go test -short ./...

# the macOS app's own unit tests (ordering, formatting — no network)
test-app:
	macos/test.sh

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

# render the app's screens, with made-up cameras, to PNG files in macos/.build/screens — to look at a change to
# a view without clicking through the app
screens:
	cd macos && swift build && "$$(swift build --show-bin-path)/IPCProbe" --snapshots "$(CURDIR)/macos/.build/screens"
	@echo "wrote macos/.build/screens/*.png"

# local multi-platform build without publishing (needs goreleaser installed)
snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf dist ipcprobe ipcprobe-* fakecam mhedpcap macos/dist macos/.build
