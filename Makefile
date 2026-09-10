.PHONY: build test vectors snapshot clean reference-test

# Go targets (once cmd/ipcprobe exists)
build:
	go build -o ipcprobe ./cmd/ipcprobe

test:
	go test ./...

# Local multi-platform build without publishing (needs goreleaser installed)
snapshot:
	goreleaser release --snapshot --clean

# Reference implementation checks (work today, no Go needed)
reference-test:
	cd reference && python3 test_ipcprobe.py

vectors:
	cd reference && python3 check_vectors.py

clean:
	rm -rf dist ipcprobe ipcprobe-*
