//go:build linux

package core

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func readStr(p string) string { b, _ := os.ReadFile(p); return string(b) }

func defaultRoute() (string, net.IP) { return parseDefaultRoute(readStr("/proc/net/route")) }

func (s *Sampler) Sample() (Metrics, error) {
	now := time.Now()
	ct, ok := parseCPU(readStr("/proc/stat"))
	if !ok {
		return Metrics{}, errors.New("no se pudo leer /proc/stat")
	}
	iface, _ := defaultRoute()
	nc, _ := parseNetDev(readStr("/proc/net/dev"), iface)
	m := Metrics{Mem: parseMeminfo(readStr("/proc/meminfo")), Net: NetRate{Iface: iface}}
	m.TempC, m.TempOf = readTemp()
	if s.hasPrev && iface == s.routeHint {
		m.CPUPct = round1(cpuPct(s.prevCPU, ct))
		dt := now.Sub(s.prevAt).Seconds()
		if dt > 0 && nc.rx >= s.prevNet.rx && nc.tx >= s.prevNet.tx {
			m.Net.RxMbps = round1(float64(nc.rx-s.prevNet.rx) * 8 / dt / 1e6)
			m.Net.TxMbps = round1(float64(nc.tx-s.prevNet.tx) * 8 / dt / 1e6)
		}
	}
	s.prevCPU, s.prevNet, s.prevAt, s.hasPrev, s.routeHint = ct, nc, now, true, iface
	if s.light {
		return m, nil
	}
	for _, me := range parseMounts(readStr("/proc/mounts")) {
		var st syscall.Statfs_t
		if syscall.Statfs(me.Mount, &st) != nil || st.Blocks == 0 {
			continue
		}
		total := float64(st.Blocks) * float64(st.Bsize) / 1e9
		free := float64(st.Bavail) * float64(st.Bsize) / 1e9
		base := baseBlockDevice(me.Device)
		kind := ""
		switch strings.TrimSpace(readStr("/sys/block/" + base + "/queue/rotational")) {
		case "0":
			kind = " · SSD"
		case "1":
			kind = " · HDD"
		}
		if strings.TrimSpace(readStr("/sys/block/"+base+"/removable")) == "1" {
			kind = " · USB"
		}
		m.Disks = append(m.Disks, Disk{Mount: me.Mount, Label: diskLabel(me.Mount), Device: strings.TrimPrefix(me.Device, "/dev/") + kind,
			TotalGB: round1(total), UsedGB: round1(total - free)})
	}
	return m, nil
}

func ReadServerInfo() ServerInfo {
	host, _ := os.Hostname()
	si := ServerInfo{Hostname: host, OS: parseOSRelease(readStr("/etc/os-release")), Kernel: strings.TrimSpace(readStr("/proc/sys/kernel/osrelease")),
		CPUModel: parseCPUModel(readStr("/proc/cpuinfo")), Cores: runtime.NumCPU()}
	if f := strings.Fields(readStr("/proc/uptime")); len(f) > 0 {
		v, _ := strconv.ParseFloat(f[0], 64)
		si.UptimeSec = int64(v)
	}
	if f := strings.Fields(readStr("/proc/loadavg")); len(f) >= 3 {
		for i := 0; i < 3; i++ {
			si.Load[i], _ = strconv.ParseFloat(f[i], 64)
		}
	}
	if iface, _ := defaultRoute(); iface != "" {
		if ip, _ := ifaceIPv4(iface); ip != nil {
			si.IP = ip.IP.String()
		}
	}
	return si
}

func readARP() []arpEntry { return parseARP(readStr("/proc/net/arp")) }

// readTemp lee la temperatura del procesador de los sensores del núcleo
// (/sys/class/hwmon): k10temp en AMD y coretemp en Intel. Si no hay, prueba
// con las zonas térmicas. No necesita instalar nada.
func readTemp() (*float64, string) {
	prefer := map[string]int{"k10temp": 3, "zenpower": 3, "coretemp": 3, "cpu_thermal": 2, "acpitz": 1}
	best, bestRank, label := -1.0, 0, ""
	dirs, _ := filepath.Glob("/sys/class/hwmon/hwmon*")
	for _, d := range dirs {
		name := strings.TrimSpace(readStr(d + "/name"))
		rank := prefer[name]
		if rank == 0 || rank < bestRank {
			continue
		}
		// temp1 es Tctl en AMD y «Package» en Intel: la del procesador entero.
		v, err := strconv.Atoi(strings.TrimSpace(readStr(d + "/temp1_input")))
		if err != nil || v <= 0 {
			continue
		}
		best, bestRank, label = float64(v)/1000, rank, name
	}
	if best < 0 {
		zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
		for _, z := range zones {
			t := strings.TrimSpace(readStr(z + "/type"))
			if t != "x86_pkg_temp" && t != "acpitz" && t != "cpu-thermal" {
				continue
			}
			if v, err := strconv.Atoi(strings.TrimSpace(readStr(z + "/temp"))); err == nil && v > 0 {
				best, label = float64(v)/1000, t
				break
			}
		}
	}
	if best < 0 || best > 150 {
		return nil, ""
	}
	r := round1(best)
	return &r, label
}
