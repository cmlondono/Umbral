package core

import (
	"testing"
	"time"
)

func TestParseAptSim(t *testing.T) {
	out := `NOTE: This is only a simulation!
Reading package lists...
Inst libssl3 [3.0.11-1~deb12u1] (3.0.13-1~deb12u1 Debian-Security:12/stable-security [amd64])
Inst tzdata [2024a-0+deb12u1] (2025b-0+deb12u1 Debian:12.11/stable [all])
Inst linux-image-amd64 (6.1.140-1 Debian:12.11/stable [amd64])
Conf libssl3 (3.0.13-1~deb12u1 Debian-Security:12/stable-security [amd64])`
	l := parseAptSim(out)
	if len(l) != 3 {
		t.Fatalf("paquetes = %d, se esperaban 3", len(l))
	}
	if !l[0].Security || l[0].Name != "libssl3" || l[0].From != "3.0.11-1~deb12u1" || l[0].To != "3.0.13-1~deb12u1" {
		t.Fatalf("primero = %+v", l[0])
	}
	if l[1].Security || l[2].Name != "linux-image-amd64" || l[2].From != "" {
		t.Fatalf("resto = %+v", l[1:])
	}
}

func TestParseSmart(t *testing.T) {
	ata := []byte(`{"device":{"type":"sat","protocol":"ATA"},"model_name":"WDC WD10EZEX","serial_number":"X","user_capacity":{"bytes":1000204886016},
		"rotation_rate":7200,"smart_status":{"passed":true},"temperature":{"current":38},"power_on_time":{"hours":21000},
		"ata_smart_attributes":{"table":[{"id":5,"raw":{"value":8}},{"id":197,"raw":{"value":0}},{"id":198,"raw":{"value":0}}]}}`)
	d := parseSmart("sda", ata)
	if d.Kind != "hdd" || d.SizeGB != 1000 || *d.TempC != 38 || *d.Reallocated != 8 || d.Health != "warn" {
		t.Fatalf("ata = %+v", d)
	}
	nvme := []byte(`{"device":{"type":"nvme","protocol":"NVMe"},"model_name":"Samsung 970","smart_status":{"passed":true},
		"nvme_smart_health_information_log":{"percentage_used":3,"media_errors":0,"critical_warning":0}}`)
	if d := parseSmart("nvme0n1", nvme); d.Kind != "nvme" || d.Health != "ok" || *d.PercentUsed != 3 {
		t.Fatalf("nvme = %+v", d)
	}
	bad := []byte(`{"smart_status":{"passed":false},"rotation_rate":0}`)
	if d := parseSmart("sdb", bad); d.Health != "fail" || d.Kind != "ssd" {
		t.Fatalf("fallo = %+v", d)
	}
	usb := []byte(`{"smart_support":{"available":false},"smartctl":{"messages":[{"string":"Unknown USB bridge","severity":"error"}]}}`)
	if d := parseSmart("sdc", usb); d.Supported || d.Health != "unknown" {
		t.Fatalf("usb = %+v", d)
	}
}

func TestSplitLogLines(t *testing.T) {
	l := splitLogLines("2026-10-28T08:00:00.123456789Z \x1b[32mhola\x1b[0m\nsin fecha\n", true)
	if len(l) != 2 || l[0].Text != "hola" || l[0].At == "" || !l[0].Err || l[1].At != "" || l[1].Text != "sin fecha" {
		t.Fatalf("líneas = %+v", l)
	}
}

func TestAvail(t *testing.T) {
	v := &Avail{d: availData{Buckets: map[string][]availBucket{}}}
	t0 := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 8; i++ {
		v.Record(t0.Add(time.Duration(i)*15*time.Second), map[string]bool{"web": i < 2 || i > 5})
	}
	views, inc := v.Views(t0.Add(2 * time.Minute))
	if len(views) != 1 || *views[0].Pct24h != 50 {
		t.Fatalf("vistas = %+v", views)
	}
	if len(inc) != 1 || inc[0].To == nil || inc[0].To.Sub(inc[0].From) != time.Minute {
		t.Fatalf("caídas = %+v", inc)
	}
	// Una caída de un solo instante no cuenta.
	v.Record(t0.Add(3*time.Minute), map[string]bool{"web": false})
	v.Record(t0.Add(3*time.Minute+15*time.Second), map[string]bool{"web": true})
	if _, inc := v.Views(t0.Add(4 * time.Minute)); len(inc) != 1 {
		t.Fatalf("una caída breve no debería quedar: %+v", inc)
	}
}

func TestShortAgent(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1": "Safari en iPhone",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36 Edg/130.0":                   "Edge en Windows",
		"curl/8.5.0": "curl",
	}
	for ua, want := range cases {
		if got := shortAgent(ua); got != want {
			t.Errorf("shortAgent(%q) = %q, se esperaba %q", ua, got, want)
		}
	}
}

func TestValidCopyDir(t *testing.T) {
	for p, ok := range map[string]bool{"": true, "/mnt/usb/umbral": true, "/media/disco": true, "/srv/x": false, "/mnt/../etc": false, "mnt/x": false} {
		if (ValidCopyDir(p) == nil) != ok {
			t.Errorf("ValidCopyDir(%q) aceptado=%v", p, !ok)
		}
	}
	if reBackup.MatchString("../x-20260101-000000.sql.gz") || !reBackup.MatchString("nextcloud-db-20261028-030000.sql.gz") {
		t.Error("nombres de copia mal validados")
	}
}

func TestNextMajorRef(t *testing.T) {
	cases := map[string]string{"nextcloud:30-apache": "nextcloud:31-apache", "nextcloud:30": "nextcloud:31", "nextcloud:30.0.2-fpm-alpine": "nextcloud:31-fpm-alpine", "ghcr.io/x/nextcloud:29": "ghcr.io/x/nextcloud:30"}
	for in, want := range cases {
		if got, ok := nextMajorRef(in); !ok || got != want {
			t.Errorf("nextMajorRef(%q) = %q, se esperaba %q", in, got, want)
		}
	}
	for _, in := range []string{"nextcloud:latest", "nextcloud:stable", "nextcloud:apache", "nextcloud"} {
		if _, ok := nextMajorRef(in); ok {
			t.Errorf("nextMajorRef(%q) no debería proponer nada", in)
		}
	}
}

func TestParseRsyncStats(t *testing.T) {
	out := "Number of files: 10 (reg: 8)\nNumber of regular files transferred: 3\nTotal file size: 2,097,152 bytes\nTotal transferred file size: 1,048,576 bytes\n" +
		"Number of regular files transferred: 1,200\nTotal file size: 10,485,760 bytes\nTotal transferred file size: 3,145,728 bytes\n"
	f, x, tot := parseRsyncStats(out)
	if f != 1203 || x != 4 || tot != 12 {
		t.Fatalf("stats = %d, %d MB, %d MB", f, x, tot)
	}
}
