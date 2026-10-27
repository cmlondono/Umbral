package core

import (
	"bufio"
	"encoding/hex"
	"net"
	"strconv"
	"strings"
)

// Funciones puras que interpretan archivos de /proc. Están separadas de la
// lectura para poder probarlas en cualquier sistema.

func parseCPU(stat string) (cpuTimes, bool) {
	for _, l := range strings.Split(stat, "\n") {
		f := strings.Fields(l)
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		var t cpuTimes
		for i, v := range f[1:] {
			n, _ := strconv.ParseUint(v, 10, 64)
			if i == 7 || i == 8 { // guest y guest_nice ya están incluidos en user y nice
				continue
			}
			t.total += n
			if i == 3 || i == 4 { // idle e iowait
				t.idle += n
			}
		}
		return t, true
	}
	return cpuTimes{}, false
}

func cpuPct(prev, cur cpuTimes) float64 {
	dt := float64(cur.total - prev.total)
	if dt <= 0 {
		return 0
	}
	p := (1 - float64(cur.idle-prev.idle)/dt) * 100
	if p < 0 {
		return 0
	}
	return p
}

func parseMeminfo(s string) MemInfo {
	kv := map[string]int{}
	for _, l := range strings.Split(s, "\n") {
		k, rest, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) > 0 {
			n, _ := strconv.Atoi(f[0])
			kv[k] = n / 1024
		}
	}
	total, free, avail := kv["MemTotal"], kv["MemFree"], kv["MemAvailable"]
	if avail == 0 {
		avail = free + kv["Buffers"] + kv["Cached"] + kv["SReclaimable"]
	}
	cache := avail - free
	if cache < 0 {
		cache = 0
	}
	return MemInfo{TotalMB: total, UsedMB: total - avail, CacheMB: cache, FreeMB: free}
}

func parseNetDev(s, iface string) (netCounters, bool) {
	for _, l := range strings.Split(s, "\n") {
		name, rest, ok := strings.Cut(l, ":")
		if !ok || strings.TrimSpace(name) != iface {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			return netCounters{}, false
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		return netCounters{rx, tx}, true
	}
	return netCounters{}, false
}

// parseDefaultRoute devuelve la interfaz y la puerta de enlace de la ruta por
// defecto a partir de /proc/net/route.
func parseDefaultRoute(s string) (iface string, gw net.IP) {
	for _, l := range strings.Split(s, "\n")[1:] {
		f := strings.Fields(l)
		if len(f) < 3 || f[1] != "00000000" {
			continue
		}
		b, err := hex.DecodeString(f[2])
		if err != nil || len(b) != 4 {
			continue
		}
		return f[0], net.IPv4(b[3], b[2], b[1], b[0])
	}
	return "", nil
}

type arpEntry struct{ IP, MAC, Iface string }

// parseARP lee /proc/net/arp y se queda con las entradas completas (flag 0x2).
func parseARP(s string) []arpEntry {
	var out []arpEntry
	sc := bufio.NewScanner(strings.NewReader(s))
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		f := strings.Fields(sc.Text())
		if len(f) < 6 || f[2] == "0x0" || f[3] == "00:00:00:00:00:00" {
			continue
		}
		out = append(out, arpEntry{IP: f[0], MAC: strings.ToUpper(f[3]), Iface: f[5]})
	}
	return out
}

func parseOSRelease(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

func parseCPUModel(s string) string {
	for _, l := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(l, ":")
		if ok && strings.TrimSpace(k) == "model name" {
			return strings.Join(strings.Fields(v), " ")
		}
	}
	return ""
}

// diskLabel da un nombre comprensible a cada punto de montaje.
func diskLabel(mount string) string {
	switch mount {
	case "/":
		return "Sistema"
	case "/home":
		return "Carpetas personales"
	case "/boot", "/boot/efi":
		return "Arranque"
	}
	parts := strings.Split(strings.Trim(mount, "/"), "/")
	l := parts[len(parts)-1]
	if l == "" {
		return mount
	}
	return strings.ToUpper(l[:1]) + l[1:]
}

var realFS = map[string]bool{"ext4": true, "ext3": true, "ext2": true, "xfs": true, "btrfs": true, "vfat": true, "exfat": true, "ntfs": true, "ntfs3": true, "f2fs": true, "zfs": true, "fuseblk": true}

type mountEntry struct{ Device, Mount, FS string }

func parseMounts(s string) []mountEntry {
	var out []mountEntry
	seen := map[string]bool{}
	for _, l := range strings.Split(s, "\n") {
		f := strings.Fields(l)
		if len(f) < 3 || !realFS[f[2]] || !strings.HasPrefix(f[0], "/dev/") {
			continue
		}
		mnt := strings.ReplaceAll(f[1], `\040`, " ")
		// Docker y snap montan los mismos dispositivos en otros sitios.
		if strings.HasPrefix(mnt, "/var/lib/docker") || strings.HasPrefix(mnt, "/snap") || mnt == "/boot/efi" || seen[f[0]] {
			continue
		}
		seen[f[0]] = true
		out = append(out, mountEntry{Device: f[0], Mount: mnt, FS: f[2]})
	}
	return out
}

// baseBlockDevice: sda1 → sda, nvme0n1p2 → nvme0n1, mmcblk0p1 → mmcblk0.
func baseBlockDevice(dev string) string {
	d := strings.TrimPrefix(dev, "/dev/")
	if strings.HasPrefix(d, "nvme") || strings.HasPrefix(d, "mmcblk") {
		if i := strings.LastIndex(d, "p"); i > 0 {
			return d[:i]
		}
		return d
	}
	return strings.TrimRight(d, "0123456789")
}

// isRandomMAC indica si una MAC es privada (aleatoria), como las de iOS y Android.
func isRandomMAC(mac string) bool {
	if len(mac) < 2 {
		return false
	}
	n, err := strconv.ParseUint(mac[:2], 16, 8)
	return err == nil && n&0x02 != 0
}
