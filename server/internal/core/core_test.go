package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseCPU(t *testing.T) {
	a, _ := parseCPU("cpu  100 0 100 800 0 0 0 0 0 0\ncpu0 1 2 3")
	b, _ := parseCPU("cpu  150 0 150 850 50 0 0 0 0 0\n")
	if p := cpuPct(a, b); p < 49 || p > 51 {
		t.Fatalf("cpuPct = %v, se esperaba 50", p)
	}
}

func TestParseMeminfo(t *testing.T) {
	m := parseMeminfo("MemTotal:        8000000 kB\nMemFree:         1000000 kB\nMemAvailable:    5000000 kB\n")
	if m.TotalMB != 7812 || m.FreeMB != 976 || m.UsedMB != 7812-4882 || m.CacheMB != 4882-976 {
		t.Fatalf("meminfo = %+v", m)
	}
}

func TestDefaultRouteAndNetDev(t *testing.T) {
	iface, gw := parseDefaultRoute("Iface\tDestination\tGateway \tFlags\nenp1s0\t00000000\t0101A8C0\t0003\nenp1s0\t0001A8C0\t00000000\t0001\n")
	if iface != "enp1s0" || gw.String() != "192.168.1.1" {
		t.Fatalf("ruta = %s %s", iface, gw)
	}
	n, ok := parseNetDev("Inter-| Receive\n face |bytes\n    lo: 10 0 0 0 0 0 0 0 10 0\nenp1s0: 2000 5 0 0 0 0 0 0 3000 4 0 0 0 0 0 0\n", "enp1s0")
	if !ok || n.rx != 2000 || n.tx != 3000 {
		t.Fatalf("netdev = %+v", n)
	}
}

func TestParseARP(t *testing.T) {
	s := "IP address       HW type     Flags       HW address            Mask     Device\n" +
		"192.168.1.1      0x1         0x2         74:da:88:10:3e:01     *        enp1s0\n" +
		"192.168.1.99     0x1         0x0         00:00:00:00:00:00     *        enp1s0\n"
	e := parseARP(s)
	if len(e) != 1 || e[0].MAC != "74:DA:88:10:3E:01" || e[0].Iface != "enp1s0" {
		t.Fatalf("arp = %+v", e)
	}
}

func TestMountsAndDevices(t *testing.T) {
	ms := parseMounts("/dev/nvme0n1p2 / ext4 rw 0 0\nproc /proc proc rw 0 0\n/dev/sda1 /srv/data ext4 rw 0 0\n/dev/sda1 /var/lib/docker/x ext4 rw 0 0\n/dev/nvme0n1p1 /boot/efi vfat rw 0 0\n")
	if len(ms) != 2 || ms[1].Mount != "/srv/data" {
		t.Fatalf("mounts = %+v", ms)
	}
	for in, want := range map[string]string{"/dev/sda1": "sda", "/dev/nvme0n1p2": "nvme0n1", "mmcblk0p1": "mmcblk0"} {
		if got := baseBlockDevice(in); got != want {
			t.Errorf("baseBlockDevice(%s) = %s", in, got)
		}
	}
	if diskLabel("/") != "Sistema" || diskLabel("/srv/data") != "Data" {
		t.Error("diskLabel")
	}
	if !isRandomMAC("DA:A1:19:5E:22:B0") || isRandomMAC("74:DA:88:10:3E:01") {
		t.Error("isRandomMAC")
	}
}

func TestCompose(t *testing.T) {
	ok := "services:\n  it-tools:\n    image: corentinth/it-tools:latest\n    ports:\n      - \"8080:80\"\n  db:\n    image: postgres:16\n"
	info, err := ParseCompose(ok)
	if err != nil || len(info.Services) != 2 || info.Ports[0] != [2]string{"8080", "80"} || len(info.Images) != 2 {
		t.Fatalf("compose = %+v %v", info, err)
	}
	if _, err := ParseCompose("version: '3'\n"); err == nil || !strings.Contains(err.Error(), "services:") {
		t.Fatal("debería pedir services:")
	}
	if _, err := ParseCompose("services:\n  a:\n    build: .\n"); err == nil || !strings.Contains(err.Error(), "image:") {
		t.Fatal("debería pedir image:")
	}
}

func TestRegistryParts(t *testing.T) {
	cases := map[string][3]string{
		"nginx:1.27":         {"registry-1.docker.io", "library/nginx", "1.27"},
		"vaultwarden/server": {"registry-1.docker.io", "vaultwarden/server", "latest"},
		"ghcr.io/home-assistant/home-assistant:stable": {"ghcr.io", "home-assistant/home-assistant", "stable"},
		"localhost:5000/app:1":                         {"localhost:5000", "app", "1"},
	}
	for in, w := range cases {
		h, p, tg := registryParts(in)
		if h != w[0] || p != w[1] || tg != w[2] {
			t.Errorf("registryParts(%s) = %s %s %s", in, h, p, tg)
		}
	}
	if !digestMatches([]string{"nginx@sha256:abc"}, "sha256:abc") || digestMatches([]string{"nginx@sha256:abc"}, "sha256:def") {
		t.Error("digestMatches")
	}
}

func TestValidImage(t *testing.T) {
	for _, s := range []string{"nginx", "nginx:1.27-alpine", "ghcr.io/home-assistant/home-assistant:stable", "localhost:5000/app:1"} {
		if !ValidImage(s) {
			t.Errorf("%s debería ser válida", s)
		}
	}
	for _, s := range []string{"", "Nginx", "nginx:", "a b", "nginx;rm -rf /"} {
		if ValidImage(s) {
			t.Errorf("%q no debería ser válida", s)
		}
	}
}

func TestMagicPacket(t *testing.T) {
	p, err := magicPacket("3C:7C:3F:A1:52:0E")
	if err != nil || len(p) != 102 || p[0] != 0xff || p[6] != 0x3c || p[101] != 0x0e {
		t.Fatalf("paquete = %x %v", p, err)
	}
	if _, err := magicPacket("no-es-una-mac"); err == nil {
		t.Fatal("debería rechazar la MAC")
	}
}

func TestAuth(t *testing.T) {
	a, err := NewAuth(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if a.SetPassword("ana", "corta") == nil {
		t.Fatal("debería exigir 10 caracteres")
	}
	if err := a.SetPassword("ana", "una-clave-larga"); err != nil {
		t.Fatal(err)
	}
	tok, _, _, err := a.Login("1.2.3.4", "ana", "una-clave-larga", "", false)
	if err != nil || tok == "" {
		t.Fatal("el inicio de sesión correcto falló", err)
	}
	if u, ok := a.Check(tok); !ok || u != "ana" {
		t.Fatal("la sesión no es válida")
	}
	for i := 0; i < maxFails; i++ {
		_, _, _, err = a.Login("5.6.7.8", "ana", "mala", "", false)
	}
	if err != ErrLocked {
		t.Fatalf("tras %d fallos debería bloquear, err = %v", maxFails, err)
	}
	if _, _, _, err := a.Login("5.6.7.8", "ana", "una-clave-larga", "", false); err != ErrLocked {
		t.Fatal("durante el bloqueo no debe entrar ni con la clave correcta")
	}
	a.Logout(tok)
	if _, ok := a.Check(tok); ok {
		t.Fatal("la sesión debería estar cerrada")
	}
}

func TestSeriesBuckets(t *testing.T) {
	s := newSeries(60, 3)
	base := time.Unix(6000, 0)
	for i := 0; i < 5; i++ {
		s.add(base.Add(time.Duration(i)*time.Minute), [nVals]float64{float64(i)})
	}
	if len(s.Vals) != 3 || s.Vals[2][0] != 3 {
		t.Fatalf("series = %+v", s.Vals)
	}
}

func TestTailscaleParse(t *testing.T) {
	js := `{"BackendState":"Running","Self":{"HostName":"servidor","DNSName":"servidor.tail1234.ts.net.","TailscaleIPs":["100.64.0.1"]},
	"Peer":{"k":{"HostName":"iphone","OS":"iOS","TailscaleIPs":["100.64.0.2"],"Online":true,"LastSeen":"0001-01-01T00:00:00Z"}}}`
	st := parseTailscale([]byte(js), TailscaleState{Installed: true})
	if !st.Running || st.Self.DNSName != "servidor.tail1234.ts.net" || len(st.Peers) != 1 || !st.Peers[0].Online || st.Peers[0].LastSeen != nil {
		t.Fatalf("tailscale = %+v", st)
	}
}

func TestAuthReloadsUsers(t *testing.T) {
	dir := t.TempDir()
	a, _ := NewAuth(dir)
	if a.Configured() {
		t.Fatal("no debería haber usuarios")
	}
	time.Sleep(20 * time.Millisecond)
	// Otro proceso (la orden de terminal) crea el usuario.
	b, _ := NewAuth(dir)
	if err := b.SetPassword("admin", "una-clave-larga"); err != nil {
		t.Fatal(err)
	}
	if !a.Configured() {
		t.Fatal("el servicio debería ver el usuario nuevo sin reiniciar")
	}
	if _, _, _, err := a.Login("1.1.1.1", "admin", "una-clave-larga", "", false); err != nil {
		t.Fatal("debería poder entrar:", err)
	}
}

func TestKeepAnonymousVolumes(t *testing.T) {
	raw := map[string]json.RawMessage{"Mounts": json.RawMessage(`[
		{"Type":"volume","Name":"abc123","Destination":"/var/www/html"},
		{"Type":"volume","Name":"nextcloud_db","Destination":"/var/lib/mysql"},
		{"Type":"bind","Source":"/srv/x","Destination":"/config"}]`)}
	host := map[string]any{"Binds": []any{"nextcloud_db:/var/lib/mysql", "/srv/x:/config"}}
	keepAnonymousVolumes(raw, host)
	b := host["Binds"].([]any)
	if len(b) != 3 || b[2] != "abc123:/var/www/html" {
		t.Fatalf("binds = %v", b)
	}
	if major("31.0.4") != 31 || major("9") != 9 || major("") != 0 {
		t.Error("major")
	}
}

func TestChangePassword(t *testing.T) {
	a, _ := NewAuth(t.TempDir())
	_ = a.SetPassword("admin", "una-clave-larga")
	if a.ChangePassword("admin", "mala-actual-xx", "otra-clave-larga") == nil {
		t.Fatal("debería exigir la contraseña actual")
	}
	if a.ChangePassword("admin", "una-clave-larga", "corta") == nil {
		t.Fatal("debería exigir 10 caracteres")
	}
	if err := a.ChangePassword("admin", "una-clave-larga", "otra-clave-larga"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := a.Login("9.9.9.9", "admin", "otra-clave-larga", "", false); err != nil {
		t.Fatal("la contraseña nueva debería valer:", err)
	}
}

func TestDemuxAndNextcloudImage(t *testing.T) {
	frame := func(kind byte, s string) []byte {
		return append([]byte{kind, 0, 0, 0, 0, 0, 0, byte(len(s))}, s...)
	}
	in := append(frame(1, "hola "), append(frame(2, "aviso"), frame(1, "mundo")...)...)
	var out, errb strings.Builder
	if err := demux(strings.NewReader(string(in)), &out, &errb); err != nil || out.String() != "hola mundo" || errb.String() != "aviso" {
		t.Fatalf("demux = %q %q %v", out.String(), errb.String(), err)
	}
	if !isNextcloudImage("nextcloud:latest") || !isNextcloudImage("nextcloud") || isNextcloudImage("nextcloud-exporter:1") {
		t.Error("isNextcloudImage")
	}
}

func TestNetNames(t *testing.T) {
	// Respuesta NetBIOS mínima con el nombre ESCRITORIO-PC (sufijo 0x00, único).
	b := make([]byte, 57)
	b[56] = 1
	entry := append([]byte("ESCRITORIO-PC  "), 0x00, 0x04, 0x00)
	if got := parseNetbios(append(b, entry...)); got != "ESCRITORIO-PC" {
		t.Fatalf("netbios = %q", got)
	}
	// Respuesta mDNS: una pregunta y un PTR que apunta a iPhone-de-Ana.local.
	msg := []byte{0, 0, 0x84, 0, 0, 1, 0, 1, 0, 0, 0, 0}
	q := []byte{2, '3', '4', 1, '1', 3, '1', '6', '8', 3, '1', '9', '2', 7, 'i', 'n', '-', 'a', 'd', 'd', 'r', 4, 'a', 'r', 'p', 'a', 0, 0, 12, 0, 1}
	msg = append(msg, q...)
	name := append([]byte{byte(len("iPhone-de-Ana"))}, "iPhone-de-Ana"...)
	name = append(name, 5, 'l', 'o', 'c', 'a', 'l', 0)
	ans := append([]byte{0xC0, 12, 0, 12, 0, 1, 0, 0, 0, 10, 0, byte(len(name))}, name...)
	msg = append(msg, ans...)
	if got := parseMDNSPtr(msg); got != "iPhone-de-Ana" {
		t.Fatalf("mdns = %q", got)
	}
	if ouiPrefix("74:da:88:10:3e:01") != "74DA88" || shortVendor("Apple, Inc.") != "Apple" || shortVendor("Samsung Electronics Co.,Ltd") != "Samsung" {
		t.Errorf("oui %q %q", shortVendor("Apple, Inc."), shortVendor("Samsung Electronics Co.,Ltd"))
	}
}

func TestTriggerState(t *testing.T) {
	temp := 82.0
	s := Snapshot{Metrics: &Metrics{CPUPct: 50, Mem: MemInfo{TotalMB: 1000, UsedMB: 900}, TempC: &temp, Disks: []Disk{{TotalGB: 100, UsedGB: 86}}},
		Docker: DockerState{Available: true, Containers: []Container{{Name: "nextcloud-app", State: "stopped", Status: "Exited (1)"}, {Name: "db", State: "running", Health: "unhealthy"}}}}
	now := time.Now()
	s.Network = NetworkState{LastScan: &now, Devices: []NetDevice{{MAC: "AA:BB:CC:DD:EE:FF", Name: "iPhone", Online: true}}}
	cases := []struct {
		t    Trigger
		want int
	}{
		{Trigger{Kind: "metric", Metric: "temp", Above: 80}, 1},
		{Trigger{Kind: "metric", Metric: "temp", Above: 85}, -1}, // 82 está en el margen (80–85): no cambia
		{Trigger{Kind: "metric", Metric: "temp", Above: 90}, 0},
		{Trigger{Kind: "metric", Metric: "mem", Above: 85}, 1},
		{Trigger{Kind: "metric", Metric: "disk", Above: 85}, 1},
		{Trigger{Kind: "metric", Metric: "cpu", Above: 90}, 0},
		{Trigger{Kind: "container", Container: "nextcloud-app", Event: "stopped"}, 1},
		{Trigger{Kind: "container", Container: "db", Event: "unhealthy"}, 1},
		{Trigger{Kind: "container", Container: "db", Event: "stopped"}, 0},
		{Trigger{Kind: "presence", MAC: "AA:BB:CC:DD:EE:FF", Event: "arrive"}, 1},
		{Trigger{Kind: "presence", MAC: "AA:BB:CC:DD:EE:FF", Event: "leave"}, 0},
	}
	for _, c := range cases {
		if got, _ := triggerState(c.t, s, map[string]bool{}); got != c.want {
			t.Errorf("%+v = %d, se esperaba %d", c.t, got, c.want)
		}
	}
	// Un contenedor con una tarea en curso (por ejemplo, actualizándose) no dispara nada.
	if got, _ := triggerState(Trigger{Kind: "container", Container: "nextcloud-app", Event: "stopped"}, s, map[string]bool{"nextcloud-app": true}); got != -1 {
		t.Error("no debe disparar mientras el panel actualiza el contenedor")
	}
}

func TestRuleValidationAndURLs(t *testing.T) {
	a, err := NewApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bad := []Rule{
		{Name: "", Trigger: Trigger{Kind: "hook"}, Steps: []Step{{Kind: "scan"}}},
		{Name: "x", Trigger: Trigger{Kind: "schedule", Time: "25:00", Days: []int{1}}, Steps: []Step{{Kind: "scan"}}},
		{Name: "x", Trigger: Trigger{Kind: "metric", Metric: "temp", Above: 10}, Steps: []Step{{Kind: "scan"}}},
		{Name: "x", Trigger: Trigger{Kind: "hook"}, Steps: []Step{}},
		{Name: "x", Trigger: Trigger{Kind: "hook"}, Steps: []Step{{Kind: "wake"}}}, // sin PC configurado
		{Name: "x", Trigger: Trigger{Kind: "hook"}, Steps: []Step{{Kind: "http", URL: "https://1.1.1.1/x"}}},
		{Name: "x", Trigger: Trigger{Kind: "hook"}, Steps: []Step{{Kind: "shell"}}},
	}
	for i, r := range bad {
		if _, _, err := a.Engine.Save(r); err == nil {
			t.Errorf("la regla %d debería rechazarse", i)
		}
	}
	r, token, err := a.Engine.Save(Rule{Name: "Escanear con NFC", Enabled: true, Trigger: Trigger{Kind: "hook"}, Steps: []Step{{Kind: "http", URL: "http://192.168.1.50:8123/api/x"}}})
	if err != nil || token == "" || r.HookHash == "" {
		t.Fatalf("regla con enlace: %v %q", err, token)
	}
	views, _ := a.Engine.Views()
	if len(views) != 1 || views[0].HookHash != "" || !views[0].HasHook {
		t.Fatal("el panel no debe ver el hash del enlace")
	}
	if _, err := a.Engine.Hook("enlace-falso"); err == nil {
		t.Fatal("un enlace falso debe rechazarse")
	}
	if checkLocalURL("http://10.0.0.5/a") != nil || checkLocalURL("http://servidor.tail1234.ts.net:8123") != nil || checkLocalURL("ftp://x") == nil {
		t.Error("checkLocalURL")
	}
}

func TestTOTP(t *testing.T) {
	// Vector de la RFC 6238 (SHA1, secreto «12345678901234567890»).
	if got := totpCode([]byte("12345678901234567890"), 59/30); got != "287082" {
		t.Fatalf("totp = %s", got)
	}
	a, _ := NewAuth(t.TempDir())
	_ = a.SetPassword("ana", "una-clave-larga")
	if _, _, err := a.TOTPBegin("ana", "mala", "Umbral"); err == nil {
		t.Fatal("debe pedir la contraseña correcta")
	}
	secret, uri, err := a.TOTPBegin("ana", "una-clave-larga", "Umbral (casa)")
	if err != nil || !strings.HasPrefix(uri, "otpauth://totp/") || !strings.Contains(uri, "secret="+secret) {
		t.Fatalf("begin: %v %s", err, uri)
	}
	raw, _ := b32.DecodeString(secret)
	now := time.Now().Unix() / totpStep
	if _, err := a.TOTPConfirm("ana", "000000"); err == nil {
		t.Fatal("un código falso no debe activarlo")
	}
	rec, err := a.TOTPConfirm("ana", totpCode(raw, now))
	if err != nil || len(rec) != 8 {
		t.Fatalf("confirm: %v %v", err, rec)
	}
	if _, _, _, err := a.Login("1.1.1.1", "ana", "una-clave-larga", "", false); err != ErrNeedCode {
		t.Fatal("con doble factor debe pedir el código", err)
	}
	if _, _, _, err := a.Login("1.1.1.1", "ana", "una-clave-larga", totpCode(raw, now), false); err == nil {
		t.Fatal("el mismo código no debe servir dos veces")
	}
	if _, _, _, err := a.Login("1.1.1.1", "ana", "una-clave-larga", totpCode(raw, now+1), false); err != nil {
		t.Fatal("el código siguiente debe valer:", err)
	}
	if _, _, _, err := a.Login("2.2.2.2", "ana", "una-clave-larga", rec[0], false); err != nil {
		t.Fatal("un código de recuperación debe valer:", err)
	}
	if _, _, _, err := a.Login("3.3.3.3", "ana", "una-clave-larga", rec[0], false); err == nil {
		t.Fatal("un código de recuperación solo vale una vez")
	}
	_ = a.SetPassword("ana", "otra-clave-larga")
	if !a.TOTPEnabled("ana") {
		t.Fatal("cambiar la contraseña no debe quitar el doble factor")
	}
	if err := a.TOTPDisable("ana", "otra-clave-larga", rec[1]); err != nil || a.TOTPEnabled("ana") {
		t.Fatal("debería poder quitarlo con contraseña y código", err)
	}
}
