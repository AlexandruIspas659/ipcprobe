# Reference implementation (Python)

The complete, tested reference for ipcprobe. Standard library only, Python 3.9+. This is the source of truth the Go
port is checked against.

- `ipcprobe.py` — the CLI (`list`, `show`, `set`).
- `test_ipcprobe.py` — unit tests over real (sanitized) frames. `python3 test_ipcprobe.py`
- `check_vectors.py` — validates `../testdata/vectors.json`. `python3 check_vectors.py`
- `compare_pcap.py` — diff a generated set-network packet against a frame in a `.pcapng` capture.
- `fake_camera.py` — loopback test double, so `list`/`show`/`set` can be exercised without hardware.
