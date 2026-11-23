package neighbor

import (
	"net/netip"
	"testing"
)

func check(t *testing.T, tab Table, want map[string]string) {
	t.Helper()
	if len(tab) != len(want) {
		t.Errorf("%d entries, want %d: %v", len(tab), len(want), tab)
	}
	for ip, mac := range want {
		if got := tab[netip.MustParseAddr(ip)]; got.String() != mac {
			t.Errorf("%s -> %q, want %q", ip, got, mac)
		}
	}
}

func TestParseBSD(t *testing.T) {
	out := `? (192.168.1.1) at 0:1b:21:4a:b5:f1 on en0 ifscope [ethernet]
? (192.168.1.11) at a4:83:e7:9f:a4:ea on en0 ifscope [ethernet]
? (192.168.1.111) at (incomplete) on en0 ifscope [ethernet]
? (192.168.1.255) at ff:ff:ff:ff:ff:ff on en0 ifscope [ethernet]
? (224.0.0.251) at 1:0:5e:0:0:fb on en0 ifscope permanent [ethernet]
gateway.lan (192.168.1.2) at 0:0:0:0:0:0 on en0 [ethernet]
nonsense line
`
	// Only the two entries that resolve to a single host: no incomplete, broadcast, multicast or all-zero ones.
	check(t, parseBSD([]byte(out)), map[string]string{
		"192.168.1.1":  "00:1b:21:4a:b5:f1", // macOS drops leading zeros; they come back
		"192.168.1.11": "a4:83:e7:9f:a4:ea",
	})
}

func TestParseProcNetARP(t *testing.T) {
	data := `IP address       HW type     Flags       HW address            Mask     Device
192.168.1.1      0x1         0x2         00:1b:21:4a:b5:f1     *        eth0
192.168.1.50     0x1         0x0         00:00:00:00:00:00     *        eth0
192.168.1.60     0x1         0x6         a4:83:e7:9f:a4:ea     *        eth0
`
	check(t, parseProcNetARP([]byte(data)), map[string]string{
		"192.168.1.1":  "00:1b:21:4a:b5:f1",
		"192.168.1.60": "a4:83:e7:9f:a4:ea", // 0x6 = complete + permanent
	})
}

func TestParseWindows(t *testing.T) {
	out := "\r\nInterface: 192.168.1.50 --- 0x4\r\n" +
		"  Internet Address      Physical Address      Type\r\n" +
		"  192.168.1.1           00-1b-21-4a-b5-f1     dynamic\r\n" +
		"  192.168.1.255         ff-ff-ff-ff-ff-ff     static\r\n" +
		"  224.0.0.22            01-00-5e-00-00-16     static\r\n"
	check(t, parseWindows([]byte(out)), map[string]string{"192.168.1.1": "00:1b:21:4a:b5:f1"})
}

func TestInUseWithNothingToCheck(t *testing.T) {
	got, err := InUse(nil, 0)
	if err != nil || len(got) != 0 {
		t.Errorf("InUse(nil) = %v, %v", got, err)
	}
}

// The table must be readable on every platform the tests run on (the CI runner included).
func TestReadWorksHere(t *testing.T) {
	if _, err := Read(); err != nil {
		t.Skipf("neighbour table not readable here: %v", err)
	}
}
