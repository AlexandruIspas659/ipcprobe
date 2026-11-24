# testdata

`example-plan.csv` — an example plan for `ipcprobe apply --plan` (and the app's Import Plan…), written for the
software cameras that `go run ./cmd/fakecam --count 5 --same-ip --ip 192.168.1.88` starts. Synthetic MACs, no
passwords. See "Plans from a spreadsheet" in the top-level README.

`vectors.json` — sanitized, language-neutral protocol test vectors. Every implementation must pass these; `go test ./internal/mhed/` is the
runnable check (`TestVectors`, plus round-trip tests that rebuild each vector from its decoded form).

Each vector has a `cmd`, the packet as `hex`, and an expectation:
- cmd 2 (announce): `expect` = the parsed `Device` fields.
- cmd 3 (set-network): `decodes_to` = the inputs that must rebuild exactly this hex (password is empty here).
- cmd 0x10 (ack): `expect_ack` = the echoed requester IP and port.
- cmd 1 (search): the exact bytes `BuildSearch()` must produce.

**These are safe to commit.** MACs, serials and device names are synthetic, all bytes outside documented fields are
zeroed, and no admin password is present (the set-network password field is all zeros).

**Never commit a real `.pcap`/`.pcapng`.** Real captures contain the admin password (base64) and real device
identifiers; `.gitignore` blocks them. If you capture new traffic to extend the protocol, decode it locally and add
a *scrubbed* vector here, don't check in the capture.
