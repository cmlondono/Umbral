package core

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseProcStat(t *testing.T) {
	s := "1234 (php-fpm: pool www) S 1200 1234 1234 0 -1 4194560 100 0 0 0 250 50 7 3 20 0 1 0 98765 300000000 5120 18446744073709551615"
	p, ok := parseProcStat(s)
	if !ok || p.pid != 1234 || p.ppid != 1200 || p.comm != "php-fpm: pool www" || p.ticks != 300 || p.child != 10 || p.start != 98765 || p.rssPg != 5120 || p.kernel {
		t.Fatalf("proceso = %+v", p)
	}
	k, _ := parseProcStat("55 (kworker/3:1) I 2 0 0 0 -1 69238880 0 0 0 0 0 4 0 0 20 0 1 0 50 0 0 18446744073709551615")
	if !k.kernel {
		t.Fatal("kworker debería ser un hilo del núcleo")
	}
}

func TestParseCgroup(t *testing.T) {
	id := strings.Repeat("ab12", 16)
	cases := map[string][2]string{
		"0::/system.slice/docker-" + id + ".scope":       {"container", id},
		"12:cpu,cpuacct:/docker/" + id + "\n0::/":        {"container", id},
		"0::/system.slice/tailscaled.service":            {"service", "tailscaled"},
		"0::/user.slice/user-1000.slice/session-3.scope": {"user", ""},
		"0::/init.scope": {"system", ""},
		"0::/system.slice/containerd.service/kubepods-besteffort": {"system", ""},
	}
	for in, want := range cases {
		if k, i := parseCgroup(in); k != want[0] || i != want[1] {
			t.Errorf("%q = %s %s, se esperaba %v", in, k, i, want)
		}
	}
}

func TestProcSampler(t *testing.T) {
	var ps procSampler
	cg := func(pid int) string {
		if pid == 20 {
			return "0::/system.slice/docker-" + strings.Repeat("c", 64) + ".scope"
		}
		return "0::/system.slice/cron.service"
	}
	names := func(id string) string { return "nextcloud-app" }
	stat := func(busy, idle uint64) string {
		return "cpu  " + itoa(busy) + " 0 0 " + itoa(idle) + " 0 0 0 0 0 0\n"
	}
	procs := []procStat{{pid: 10, ppid: 1, comm: "cron", ticks: 5}, {pid: 20, ppid: 1, comm: "php", ticks: 100, rssPg: 256}, {pid: 21, ppid: 20, comm: "php", ticks: 0}}
	if _, ok := ps.sampleFrom(stat(1000, 1000), procs, cg, names, 4); ok {
		t.Fatal("la primera lectura solo es de referencia")
	}
	// 1000 ticks después: php usa 600 (60 %); cron lanzó un hijo que ya
	// terminó y gastó 100 (10 %); el hijo 21 de php terminó tras gastar 50,
	// de los que 0 ya se habían contado.
	procs = []procStat{{pid: 10, ppid: 1, comm: "cron", ticks: 5, child: 100}, {pid: 20, ppid: 1, comm: "php", ticks: 650, child: 50, rssPg: 256}}
	n, ok := ps.sampleFrom(stat(1800, 1200), procs, cg, names, 4)
	if !ok || n.Total != 80 {
		t.Fatalf("total = %v %v", n.Total, ok)
	}
	if n.Procs[0].Name != "php" || n.Procs[0].Group != "nextcloud-app" || n.Procs[0].CPU != 60 || n.Procs[0].MemMB != 1 {
		t.Fatalf("primero = %+v", n.Procs[0])
	}
	if n.Procs[1].Name != "cron" || n.Procs[1].Kind != "service" || n.Procs[1].CPU != 10 {
		t.Fatalf("segundo = %+v", n.Procs[1])
	}
}

func itoa(v uint64) string { return strconv.FormatUint(v, 10) }

func TestCPUWatchPeaks(t *testing.T) {
	w := newCPUWatch(t.TempDir())
	at := time.Unix(1_800_000_000, 0)
	rec := func(total float64, procs ...ProcUse) bool {
		at = at.Add(15 * time.Second)
		return w.Record(CPUNow{At: at, Total: total, Split: CPUSplit{User: total}, Procs: procs}, 15*time.Second)
	}
	php := ProcUse{Name: "php", Group: "nextcloud-app", Kind: "container", CPU: 80, MemMB: 300, Count: 4}
	rec(20)
	rec(90, php)
	rec(75, ProcUse{Name: "php", Group: "nextcloud-app", Kind: "container", CPU: 60, MemMB: 200, Count: 6}, ProcUse{Name: "mariadbd", Kind: "container", CPU: 10})
	rec(40) // un bajón no cierra el pico
	if v := w.View(); len(v.Peaks) != 1 || !v.Peaks[0].Open {
		t.Fatalf("picos = %+v", v.Peaks)
	}
	if !rec(30) {
		t.Fatal("dos lecturas por debajo deberían cerrar el pico")
	}
	pk := w.View().Peaks[0]
	if pk.Open || pk.N != 2 || pk.Max != 90 || pk.Avg != 82.5 || pk.To.Sub(pk.From) != 30*time.Second {
		t.Fatalf("pico = %+v", pk)
	}
	if pk.Top[0].Name != "php" || pk.Top[0].CPU != 70 || pk.Top[0].MemMB != 300 || pk.Top[0].Count != 6 || pk.Top[1].CPU != 5 {
		t.Fatalf("causas = %+v", pk.Top)
	}
	if w.SetThreshold(60) || !w.SetThreshold(90) {
		t.Fatal("solo se admiten 50, 70 y 90")
	}
	rec(80)
	if len(w.View().Peaks) != 1 {
		t.Fatal("80 % no llega al nuevo umbral de 90 %")
	}
}
