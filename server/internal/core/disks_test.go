package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeBlock crea en dir un /sys/block, una base de udev y un mountinfo de
// prueba. parts: nombre de la partición → propiedades de udev.
type fakeDisk struct {
	name, devnum, bus string
	sectors           string
	props             map[string]string
	parts             []fakePart
}
type fakePart struct {
	name, devnum string
	props        map[string]string
	holder       bool
}

func fakeEnv(t *testing.T, disks []fakeDisk, mountinfo, fstab string) diskEnv {
	dir := t.TempDir()
	e := diskEnv{filepath.Join(dir, "block"), filepath.Join(dir, "udev"), filepath.Join(dir, "mountinfo"), filepath.Join(dir, "fstab")}
	w := func(p, s string) {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	udev := func(devnum string, props map[string]string) {
		var b strings.Builder
		for k, v := range props {
			b.WriteString("E:" + k + "=" + v + "\n")
		}
		w(filepath.Join(e.udev, "b"+devnum), b.String())
	}
	for _, d := range disks {
		base := filepath.Join(e.sys, d.name)
		w(filepath.Join(base, "dev"), d.devnum)
		w(filepath.Join(base, "size"), d.sectors)
		w(filepath.Join(base, "device", "vendor"), "WD      ")
		w(filepath.Join(base, "device", "model"), "Elements 25A2   ")
		p := map[string]string{"ID_BUS": d.bus, "ID_SERIAL_SHORT": "S" + d.name}
		for k, v := range d.props {
			p[k] = v
		}
		udev(d.devnum, p)
		for _, pt := range d.parts {
			pb := filepath.Join(base, pt.name)
			w(filepath.Join(pb, "partition"), "1")
			w(filepath.Join(pb, "dev"), pt.devnum)
			w(filepath.Join(pb, "size"), "976771072")
			if pt.holder {
				w(filepath.Join(pb, "holders", "dm-0"), "")
			}
			udev(pt.devnum, pt.props)
		}
	}
	w(e.mountinfo, mountinfo)
	w(e.fstab, fstab)
	return e
}

const mi = "22 1 8:2 / / rw,relatime shared:1 - ext4 /dev/sda2 rw\n"

func TestDetectDisks(t *testing.T) {
	ext := map[string]string{"ID_FS_TYPE": "ext4", "ID_FS_UUID": "1234abcd-0000", "ID_FS_LABEL": "copias"}
	e := fakeEnv(t, []fakeDisk{
		{name: "sda", devnum: "8:0", bus: "ata", sectors: "1953525168", parts: []fakePart{{name: "sda2", devnum: "8:2", props: map[string]string{"ID_FS_TYPE": "ext4"}}}},
		{name: "sdb", devnum: "8:16", bus: "usb", sectors: "976773168", parts: []fakePart{{name: "sdb1", devnum: "8:17", props: map[string]string{"ID_FS_TYPE": "ntfs", "ID_FS_LABEL": "Mi disco"}}}},
		{name: "sdc", devnum: "8:32", bus: "usb", sectors: "976773168", parts: []fakePart{{name: "sdc1", devnum: "8:33", props: ext}}},
		{name: "sdd", devnum: "8:48", bus: "usb", sectors: "976773168", parts: []fakePart{{name: "sdd1", devnum: "8:49", props: ext}}},
		{name: "sde", devnum: "8:64", bus: "usb", sectors: "976773168", parts: []fakePart{{name: "sde1", devnum: "8:65", props: map[string]string{"ID_FS_TYPE": "vfat"}}}},
		{name: "sdf", devnum: "8:80", bus: "usb", sectors: "976773168", parts: []fakePart{{name: "sdf1", devnum: "8:81", props: map[string]string{"ID_FS_TYPE": "LVM2_member"}}}},
		{name: "sdg", devnum: "8:96", bus: "usb", sectors: "976773168"},
		{name: "sdh", devnum: "8:112", bus: "usb", sectors: "976773168", parts: []fakePart{{name: "sdh1", devnum: "8:113", props: map[string]string{"ID_FS_TYPE": "ext4"}}}},
		{name: "loop0", devnum: "7:0", bus: "usb", sectors: "100"},
		{name: "sdi", devnum: "8:128", bus: "usb", sectors: "0"},
	}, mi+
		"30 22 8:49 / /mnt/copias rw - ext4 /dev/sdd1 rw\n"+
		"31 22 8:65 / /media/usb\\040key rw - vfat /dev/sde1 rw\n"+
		"32 22 8:113 / /srv rw - ext4 /dev/sdh1 rw\n",
		"# fstab\nUUID=1234abcd-0000 /mnt/copias ext4 defaults,nofail 0 2\n")
	got := map[string]ExtDisk{}
	for _, d := range e.detect() {
		got[d.Name] = d
	}
	if _, ok := got["sda"]; ok {
		t.Fatal("el disco interno no debe aparecer")
	}
	for _, n := range []string{"loop0", "sdi"} {
		if _, ok := got[n]; ok {
			t.Fatal(n + " no debe aparecer")
		}
	}
	want := map[string]string{"sdb": "other", "sdc": "ext4", "sdd": "mounted", "sde": "busy", "sdf": "busy", "sdg": "other", "sdh": "busy"}
	for n, s := range want {
		if got[n].State != s {
			t.Errorf("%s: estado %q, quiero %q (%s)", n, got[n].State, s, got[n].Problem)
		}
	}
	if d := got["sdb"]; !d.CanFormat || d.Model != "WD Elements 25A2" || d.Serial != "Ssdb" || d.SizeBytes != 976773168*512 || d.Parts[0].Label != "Mi disco" {
		t.Errorf("sdb: %+v", d)
	}
	if d := got["sdc"]; !d.Known || !d.CanFormat {
		t.Errorf("sdc debe figurar en fstab y poderse formatear: %+v", d)
	}
	if d := got["sdd"]; d.Mount != "/mnt/copias" || d.CanFormat {
		t.Errorf("sdd: %+v", d)
	}
	if d := got["sde"]; d.CanFormat || !strings.Contains(d.Problem, "umount /media/usb key") {
		t.Errorf("sde debe pedir desmontarlo: %+v", d)
	}
	if d := got["sdh"]; d.CanFormat || !strings.Contains(d.Problem, "/srv") {
		t.Errorf("sdh lo usa el sistema: %+v", d)
	}
	if len(got["sdg"].Parts) != 0 {
		t.Error("un disco vacío no tiene particiones")
	}
}

func TestPartDevAndUnescape(t *testing.T) {
	if partDev("sdb") != "sdb1" || partDev("nvme0n1") != "nvme0n1p1" || partDev("mmcblk0") != "mmcblk0p1" {
		t.Fatal("nombres de partición")
	}
	if unescapeMount(`/media/mi\040disco`) != "/media/mi disco" || unescapeMount(`/a\`) != `/a\` {
		t.Fatal("unescape")
	}
	if fstabPoint([]fstabEnt{{"/dev/disk/by-uuid/abc", "/mnt/x"}}, "abc") != "/mnt/x" || fstabPoint(nil, "") != "" {
		t.Fatal("fstabPoint")
	}
}

func TestPasswdIDs(t *testing.T) {
	p := filepath.Join(t.TempDir(), "passwd")
	_ = os.WriteFile(p, []byte("root:x:0:0:root:/root:/bin/sh\numbral:x:998:997::/nonexistent:/usr/sbin/nologin\n"), 0o644)
	if u, g, ok := passwdIDs(p, "umbral"); !ok || u != 998 || g != 997 {
		t.Fatal(u, g, ok)
	}
	if _, _, ok := passwdIDs(p, "nadie"); ok {
		t.Fatal("no existe")
	}
}

func TestDiskRequestRejected(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	// Sin petición no hace nada.
	if err := RunDiskHelper(dir, out); err != nil {
		t.Fatal(err)
	}
	// Una petición con un identificador raro se descarta y se borra.
	_ = os.WriteFile(filepath.Join(dir, diskReqName), []byte(`{"id":"../x","action":"formatear"}`), 0o600)
	if err := RunDiskHelper(dir, out); err == nil {
		t.Fatal("debe rechazarla")
	}
	if _, err := os.Stat(filepath.Join(dir, diskReqName)); err == nil {
		t.Fatal("la petición debe borrarse")
	}
	// Una petición caducada no toca nada y lo explica.
	_ = os.WriteFile(filepath.Join(dir, diskReqName), []byte(`{"id":"0123456789ab","action":"formatear","disk":"sdz","at":"2020-01-01T00:00:00Z"}`), 0o600)
	_ = RunDiskHelper(dir, out)
	b, _ := os.ReadFile(filepath.Join(out, diskResultName))
	var res diskResult
	if json.Unmarshal(b, &res) != nil || !res.Done || !strings.Contains(res.Error, "caducó") {
		t.Fatalf("resultado: %s", b)
	}
}

func TestVerifyCode(t *testing.T) {
	a, _ := NewAuth(t.TempDir())
	_ = a.SetPassword("ana", "una-clave-larga")
	if err := a.VerifyCode("ana", "123456"); err != ErrNeed2FA {
		t.Fatal("sin doble factor activo la acción no se permite:", err)
	}
	secret, _, _ := a.TOTPBegin("ana", "una-clave-larga", "Umbral")
	raw, _ := b32.DecodeString(secret)
	now := time.Now().Unix() / totpStep
	if _, err := a.TOTPConfirm("ana", totpCode(raw, now)); err != nil {
		t.Fatal(err)
	}
	if err := a.VerifyCode("ana", ""); err != ErrNeedCode {
		t.Fatal("debe pedir el código:", err)
	}
	if err := a.VerifyCode("ana", totpCode(raw, now+1)); err != nil {
		t.Fatal("un código válido debe valer:", err)
	}
	if err := a.VerifyCode("ana", totpCode(raw, now+1)); err == nil {
		t.Fatal("el mismo código no vale dos veces")
	}
	for i := 0; i < maxFails; i++ {
		_ = a.VerifyCode("ana", "000000")
	}
	if err := a.VerifyCode("ana", "000000"); err == nil || !strings.Contains(err.Error(), "Espera") {
		t.Fatal("tras varios fallos debe bloquearse:", err)
	}
}
