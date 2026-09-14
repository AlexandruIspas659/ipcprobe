# How ipcprobe works

A walkthrough of the Go implementation, top to bottom, for someone who wants to understand or change it. Read
[PROTOCOL.md](../PROTOCOL.md) first if you don't yet know what the packets look like; this document is about the
code that sends and receives them.

## 1. The shape of the program

```
cmd/ipcprobe/            the executable: argument parsing, printing, exit codes
    main.go              list / show / set
    password.go          reading the admin password without echo
cmd/fakecam/             dev tool: run a software camera on an interface
cmd/mhedpcap/            dev tool: list / decode / diff MHED frames in a capture
internal/mhed/           the protocol: bytes <-> structs. No sockets, no I/O.
    mhed.go              parsers and builders, both directions
    mhed_test.go         driven by testdata/vectors.json
internal/discovery/      the network: interfaces, multicast sockets, the receive loop
    discovery.go
    discovery_test.go    end-to-end test against the fake camera, over real sockets
internal/fakecam/        the device side of the protocol (announce, ack, apply on correct password)
testdata/vectors.json    known-good packets with their decoded meaning
```

Three layers, each only allowed to depend on the one below it:

```
cmd/ipcprobe  ──uses──▶  internal/discovery  ──uses──▶  internal/mhed
 (what the user           (how packets get on          (what the bytes
  asked for)               and off the wire)            mean)
```

`internal/` is a Go convention: packages under it can only be imported from inside this module. That's deliberate —
the protocol and socket code are not a public library (yet); the only supported interface is the command line.

The whole thing uses the Go **standard library only**. There is no `require` line in `go.mod`, so `go build`
needs no network access and there is nothing to audit but Go itself. The binary is statically linked
(`CGO_ENABLED=0` at release time): one file, no runtime, no shared libraries.

## 2. `internal/mhed` — the protocol layer

This package answers exactly one question: *given bytes, what do they mean; given meaning, what are the bytes?*
It never opens a socket, which is what makes it trivially testable and portable.

### Constants

Everything about the wire format is a named constant at the top of `mhed.go`: the port (`23456`), the two
multicast groups (`GroupTX` is where the tool sends, `GroupRX` is where devices talk), the four command codes,
the two header "version" words, and every field offset. If PROTOCOL.md ever changes, this block is the only place
offsets live — the functions below never contain a bare number.

### Packet structure

Every packet starts with the same 10 bytes:

```
offset 0   "MHED"            4 bytes  magic
offset 4   version           uint16 LE   0x0009 tool→device, 0x0008 device→tool
offset 6   0x0001            uint16 LE   constant
offset 8   command           uint16 LE   1 search, 2 announce, 3 set-network, 0x10 ack
```

`header(cmd)` builds that prefix for outbound packets. `Command(pkt)` is the inverse: it checks the magic and
returns the command word, and is the first thing the receive loop calls on every datagram to decide whether it's
even ours.

**Little-endian** matters. The camera firmware writes multi-byte integers low byte first, so `0x0003` is on the wire
as `03 00`. All reads and writes go through `binary.LittleEndian` so nobody has to remember that in the callers.
The IPv4 addresses are the exception: they are stored as their 4 raw bytes in network order, exactly as
`netip.Addr.As4()` gives them.

### Building outbound packets

`BuildSearch()` returns the 140-byte probe: the header with command 1, and 130 zero bytes. That's it — the probe
carries no payload.

`BuildSetNetwork(mac, ip, mask, gw, dns1, dns2, password)` returns the 140-byte set-network packet. It allocates
140 zero bytes (`make([]byte, lenTX)` zero-fills), writes the header, then copies each field to its offset:

```
32  MAC (6 bytes)         which camera this is for
40  new IP                 }
44  new netmask            } 4 raw bytes each
48  new gateway            }
84  password, 28 bytes     base64 of the plaintext, NUL-padded
112 DNS1
116 DNS2
```

Every byte not listed stays zero — this was verified byte-for-byte against real IPTool packets, so "zero" is not a
guess. The function validates before it writes: the MAC must be 6 bytes and every address must be IPv4, otherwise
it returns an error rather than a malformed packet.

`EncodePassword` is separate because the CLI calls it early, before any networking, so a too-long password fails
fast. The field is 28 bytes of base64, and base64 turns every 3 input bytes into 4 output characters, so the longest
password that fits is 21 bytes (`MaxPasswordBytes`). An empty password encodes to an all-zero field.

A thing to notice: base64 is **encoding, not encryption**. The password is readable by anyone who captures the packet.
That is a property of the camera firmware, documented in PROTOCOL.md; the tool can't do anything about it except
never store or print it.

### Parsing inbound packets

`ParseAnnounce(pkt)` turns a 240-byte cmd-2 announcement into a `Device` struct. It refuses (returns `ok=false`)
if the magic is wrong, the command isn't 2, or the packet is short — so the receive loop can feed it *anything* and
only get a struct back for real announcements. The fields it extracts are exactly the table in PROTOCOL.md: name,
MAC, IP, mask, gateway, firmware build date, HTTP and RTSP ports, DNS1/2, serial, firmware, model, vendor.

Two helpers do the fiddly work. `cstr(pkt, off, n)` reads a fixed-width NUL-padded string: it takes `n` bytes and
cuts at the first zero byte, which is how the camera stores text (`"DVC\0\0\0..."`). `buildDate` reads a uint32 that
the firmware stores as a decimal `YYYYMMDD` number (e.g. `20231103`), splits it arithmetically, sanity-checks the
ranges, and formats it as `2023-11-03`; if the number doesn't look like a date it returns `""` rather than garbage.

`ParseSetAck(pkt)` handles the cmd-`0x10` packet a camera sends right after it receives a set-network command.
It contains only the requester's IP and UDP port echoed back. The important thing about this ack — learned the hard
way — is that it means *received*, not *applied*: a camera sends it even when the password was wrong. So the CLI
uses it as a progress signal, never as a success signal.

### `Device` and JSON

`Device` has JSON tags (`json:"mac"`, …) whose names match the keys the Python reference used, so `--json` output is
stable and `vectors.json` can be decoded straight into the struct in tests. `Src` — the IP the announcement came
from — is filled in by the discovery layer, not the parser, and is `omitempty` because it isn't part of the packet.

### Building the device side too

`BuildAnnounce` and `BuildSetAck` are the exact inverses of the two parsers. They exist for the fake camera and the
tests, not for the CLI, but they earn their keep: the announce vectors have every undocumented byte zeroed, so
`BuildAnnounce(ParseAnnounce(x))` must equal `x` exactly — a stronger check on the offsets than parsing alone.

### The test

`mhed_test.go` loads `../../testdata/vectors.json` and, for every vector, checks the appropriate direction:
announcements must parse to exactly the expected struct, the search bytes must equal `BuildSearch()`, the
set-network vector must be reproduced by `BuildSetNetwork` from its decoded inputs, and the ack must parse to the
expected requester. This one file is the conformance contract: any implementation in any language that passes it
speaks the protocol correctly. The rest of the tests cover the edges — password length limit, empty password,
build-date validation, rejecting non-announcements.

## 3. `internal/discovery` — the network layer

This is the only part of the program that touches sockets, and the only part with operating-system differences to
think about.

### Why multicast is the whole problem

The protocol is **link-layer multicast**. A search goes to `234.55.55.55`; cameras answer on `234.55.55.56`. Routers
don't forward these packets, and the cameras send with TTL 1, so a packet only ever reaches machines on the same
Ethernet segment as the interface it left from. This is *why* the tool can reach a camera whose IP is on the wrong
subnet — IP doesn't matter at this layer — and also why it cannot work over a VPN or from a different VLAN. The
design of this package follows from that: everything is per-interface.

### Interfaces: `Interfaces()` and `Choose()`

`Interfaces()` asks the OS for its network interfaces and keeps the ones that are up, not loopback, multicast-capable,
and have an IPv4 address. It returns an `Iface` per address: name (`en0`), the IP, the subnet (`192.168.1.0/24`) and
the underlying `*net.Interface` needed later to join a group on it. A Mac typically has several — Wi‑Fi, wired,
Thunderbolt bridge, VPN tunnels — and the filter throws out the ones that can't do multicast (tunnels are
point-to-point without the multicast flag).

`Choose(ifaceArg, prefer...)` decides which of those to use:

1. If the user gave `--iface`, that wins. It accepts a name (`en0`) or an IPv4 address. If the name exists but
   was filtered out, `whyUnusable` explains why — down, loopback, or the important case: a VPN/point-to-point
   interface, with a message that says the protocol can't cross a tunnel.
2. Otherwise, if any "preferred" address falls inside an interface's subnet, those interfaces are used. `set` passes
   the camera's *new* IP here, so a camera being moved to 10.0.0.x is sent from an interface on 10.0.0.x if one
   exists.
3. Otherwise, all of them. This is the `list` default, and it's also what happens for the foreign-subnet rescue,
   where no interface matches — which is correct, because the packet needs to go out on the segment regardless of IP.

### Sockets: `Open()`

For each chosen interface, `Open` creates one receive socket with `net.ListenMulticastUDP("udp4", iface, group)`.
That single standard-library call does three things that would otherwise need raw `setsockopt`s: it binds to
`234.55.55.56:23456`, sets address reuse so several sockets can share the port, and joins the multicast group *on
that specific interface* (`IP_ADD_MEMBERSHIP` with the interface's address). Joining tells the OS and the switch
"deliver this group's traffic here" — without it the announcements are filtered out by the NIC before software ever
sees them.

Receive buffers are enlarged to 1 MB so a burst of announcements from a large fleet isn't dropped. If an interface
refuses the join (some virtual interfaces do), `Open` warns and skips it, failing only if *none* joined — a Mac with
five interfaces shouldn't be unusable because one of them is odd.

### Sending: `send()`, `Probe()`, `SendSet()`

Sending is done per interface, with a fresh short-lived socket each time: `net.DialUDP("udp4", local, dst)` where
`local` is the interface's own IP and `dst` is `234.55.55.55:23456`. **Binding the local address is what selects the
egress interface.** With a multicast destination the OS has no routing table entry to consult; it uses the source
address you bound to decide which NIC the packet leaves on. This is the standard-library equivalent of the
`IP_MULTICAST_IF` socket option and the reason the tool doesn't need the `x/net/ipv4` package.

`Probe()` sends the search packet; `SendSet()` sends a prebuilt set-network packet. Both are the same operation with
a different payload.

### Receiving: `Collect()`

`Collect(d)` reads for `d` seconds. One goroutine per receive socket loops on `ReadFromUDP` with a deadline; every
datagram is offered to `mhed.ParseAnnounce` and, failing that, `mhed.ParseSetAck`; anything else is ignored. Results
are merged under a mutex into a `map[MAC]*Device` — keyed by MAC, not IP, because the IP is the thing that changes
during a `set`, and "newest wins" so a camera that moves shows its latest address. Acks are appended to a slice with
the sender's IP. When the deadline passes, every goroutine returns and `Collect` hands back the map and the acks.

`Discover(d)` is just `Probe()` then `Collect(d)`.

### The end-to-end test

`discovery_test.go` starts an `internal/fakecam` camera on the first usable interface and runs the real thing
against it over real sockets: discover it, send a set-network with the wrong password (expect an ack and no
change), send it with the right password (expect an ack and a re-announcement at the new address). This is the
same sequence that was verified against physical cameras, and it runs in about three seconds on every `go test`.
It skips itself on machines with no multicast-capable interface rather than failing.

The two sort helpers exist because a map has no order: the table is sorted by IP (natural for a human), the JSON by
MAC (stable across runs, natural for a script).

## 4. `cmd/ipcprobe` — the command line

### Dispatch and flags

`main` hands `os.Args[1:]` to `run`, which switches on the first word: `list`, `show`, `set`, `help`, `version`. The
exit code is whatever the command returns, and the codes are part of the interface: `0` success, `1` a setup or
environment problem (no interface, camera not found), `2` a usage error *or* a set that could not be confirmed.
Scripts can rely on those.

The flag parser is a small custom one rather than Go's `flag` package, because `flag` treats `--json` and `-json`
alike but is awkward about `--key value` vs `--key=value` mixed with boolean flags. `parseFlags` takes a set of
boolean flag names (so `--wide` doesn't eat the next argument) and returns a map; `f.str`, `f.has`, `f.dur` read from
it with defaults.

`version` is a plain variable defaulting to `"dev"`. The release build overwrites it at link time with
`-ldflags "-X main.version=0.1.1"`, which is why `ipcprobe --version` prints the real tag without any code knowing
the tag.

### `list`

Choose interfaces → open sockets → say what we're listening on (to stderr, so `--json` output stays clean on stdout)
→ `Discover` for `--timeout` seconds → print. `printTable` computes column widths from the data so the table lines
up; `--wide` appends HTTP/RTSP/Build columns. `--json` marshals the sorted slice of `Device`.

### `show`

Same discovery, then picks one MAC out of the map and prints every field as `key : value`, adding derived
`http://IP/` and `rtsp://IP/` URLs (with the port only if it isn't the default). Exit 1 if the MAC wasn't seen.

### `set` — the one that matters

This is deliberately the most careful code path, because it changes a device you may not be able to reach any other
way.

1. **Validate everything before touching the network.** MAC, IP, mask and gateway must parse; the mask must be a
   contiguous netmask (`net.IPMask.Size()` returns 0 for `255.0.255.0`); the IP must not be the network or broadcast
   address of its subnet; a gateway outside the subnet is allowed but warned about. DNS1 defaults to the gateway,
   DNS2 to `8.8.8.8`.
2. **Get the password** — from `--password` if given, otherwise prompted without echo — and check it fits the
   28-byte field. All of this happens before any socket exists, so a typo costs nothing.
3. **Discover first, and refuse an unseen MAC.** The tool probes for `--timeout` seconds (default 3) and looks for the
   target MAC. If it isn't announcing, `set` stops with exit 1 — a set-network packet to a MAC that isn't there is
   never useful, and typing the wrong MAC is the most likely mistake. `--force` overrides this for the case where
   you know better.
4. **Build and send.** `BuildSetNetwork` → `--dump` (prints the packet as hex with the password bytes masked as
   `**`, so a debug log never leaks it) → `SendSet` on the chosen interfaces.
5. **Confirm.** Up to `--confirm-timeout` seconds (default 15), the loop re-probes and collects in 2‑second windows.
   An ack sets a flag and prints "received the command" once. Success is *only* the target MAC announcing with the
   requested IP — that line goes to stdout and the exit code is 0. If the deadline passes: exit 2 with a hint that
   depends on whether an ack was seen. No ack → the packet probably never reached the camera (wrong interface, wrong
   VLAN). Ack but no re-announce → the camera got it but didn't apply it, which is almost always a wrong password —
   or occasionally just a slow camera, which is why the message says to run `list` rather than asserting failure.

Observed on real hardware: a camera re-announces at its new address within a few seconds on the same subnet, and
can take longer when changing subnet; 15 s covers both with margin.

### `password.go`

Go's standard library has no "read a line without echo". The usual answer is `golang.org/x/term`, but that would be
the project's only dependency. Instead, on macOS and Linux the tool runs `stty -echo` before reading a line from
stdin and `stty echo` after, which is what `x/term` does underneath anyway. If `stty` is missing (or on Windows) it
warns and reads with echo rather than refusing to work.

## 5. Things the OS does that the code has to live with

- **macOS Local Network permission.** The first time a program sends multicast, macOS asks the user to allow it, per
  *responsible application* — for a terminal tool that means Terminal/iTerm. Deny it and every send silently goes
  nowhere. A future GUI app must declare `NSLocalNetworkUsageDescription` in its Info.plist or the prompt never
  appears at all.
- **Multicast loopback is off** by default for `ListenMulticastUDP`, so the tool doesn't hear its own probes. This
  is correct in production and is why a same-host fake camera needs its own sending socket.
- **TTL.** The tool leaves the default multicast TTL (1). IPTool uses 5. On-segment it makes no difference; the
  protocol never crosses a router either way.
- **Gatekeeper.** An unsigned binary downloaded with a browser carries a quarantine attribute and macOS refuses to run
  it. Homebrew removes it; a manual download needs `xattr -d com.apple.quarantine ipcprobe`. Signing and notarizing
  the binary would remove the problem entirely and requires an Apple Developer ID.

## 6. Where to change things

- A new field decoded from the announcement: add the offset constant and the `Device` field in `mhed.go`, add it to
  the expected structs in `testdata/vectors.json`, then surface it in `printTable`/`show`.
- A new command the protocol turns out to support: add the command constant, a `BuildX` function, a vector, and a
  `cmdX` in `main.go`. Nothing in `discovery` should need to change — it already forwards every MHED datagram.
- Different socket behaviour (TTL, loopback for testing): `Open` and `send` in `discovery.go` are the only places.
