package core

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

/* Nombres de los dispositivos de la red. Los routers del proveedor casi
 * nunca resuelven nombres por DNS, así que se pregunta a cada equipo:
 *  - NetBIOS (UDP 137): responden los PC con Windows y los NAS.
 *  - mDNS/Bonjour (UDP 5353): iPhone, Mac, impresoras, televisores, Chromecast.
 * Y el fabricante sale de la lista oficial de la IEEE (se descarga una vez). */

// netbiosName pide el «node status» de NetBIOS y devuelve el nombre del equipo.
func netbiosName(ip string, timeout time.Duration) string {
	c, err := net.DialTimeout("udp4", net.JoinHostPort(ip, "137"), timeout)
	if err != nil {
		return ""
	}
	defer c.Close()
	q := []byte{0x13, 0x37, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0x20, 'C', 'K'}
	for i := 0; i < 30; i++ {
		q = append(q, 'A')
	}
	q = append(q, 0, 0, 0x21, 0, 1)
	_ = c.SetDeadline(time.Now().Add(timeout))
	if _, err := c.Write(q); err != nil {
		return ""
	}
	buf := make([]byte, 1024)
	n, err := c.Read(buf)
	if err != nil {
		return ""
	}
	return parseNetbios(buf[:n])
}

func parseNetbios(b []byte) string {
	// Cabecera (12) + nombre (34) + tipo, clase, TTL y longitud (10) = 56.
	if len(b) < 57 {
		return ""
	}
	count := int(b[56])
	for i := 0; i < count; i++ {
		off := 57 + i*18
		if off+18 > len(b) {
			break
		}
		name := strings.TrimRight(string(b[off:off+15]), " \x00")
		suffix, flags := b[off+15], binary.BigEndian.Uint16(b[off+16:off+18])
		// Sufijo 0x00 sin el bit de grupo: el nombre del equipo.
		if suffix == 0 && flags&0x8000 == 0 && name != "" {
			return name
		}
	}
	return ""
}

// mdnsName pregunta por mDNS unicast el nombre asociado a una IP (PTR).
func mdnsName(ip string, timeout time.Duration) string {
	p := net.ParseIP(ip).To4()
	if p == nil {
		return ""
	}
	c, err := net.DialTimeout("udp4", net.JoinHostPort(ip, "5353"), timeout)
	if err != nil {
		return ""
	}
	defer c.Close()
	q := []byte{0x00, 0x00, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, l := range []string{fmt.Sprint(p[3]), fmt.Sprint(p[2]), fmt.Sprint(p[1]), fmt.Sprint(p[0]), "in-addr", "arpa"} {
		q = append(q, byte(len(l)))
		q = append(q, l...)
	}
	q = append(q, 0, 0, 12, 0, 1)
	_ = c.SetDeadline(time.Now().Add(timeout))
	if _, err := c.Write(q); err != nil {
		return ""
	}
	buf := make([]byte, 1500)
	n, err := c.Read(buf)
	if err != nil {
		return ""
	}
	return parseMDNSPtr(buf[:n])
}

// parseMDNSPtr lee el primer registro PTR de la respuesta.
func parseMDNSPtr(b []byte) string {
	if len(b) < 12 {
		return ""
	}
	qd, an := int(binary.BigEndian.Uint16(b[4:6])), int(binary.BigEndian.Uint16(b[6:8]))
	off := 12
	for i := 0; i < qd; i++ {
		_, o, ok := dnsName(b, off)
		if !ok {
			return ""
		}
		off = o + 4
	}
	for i := 0; i < an; i++ {
		_, o, ok := dnsName(b, off)
		if !ok || o+10 > len(b) {
			return ""
		}
		typ := binary.BigEndian.Uint16(b[o : o+2])
		rdlen := int(binary.BigEndian.Uint16(b[o+8 : o+10]))
		data := o + 10
		if typ == 12 {
			name, _, ok := dnsName(b, data)
			if ok {
				return cleanHostname(name)
			}
		}
		off = data + rdlen
	}
	return ""
}

// dnsName lee un nombre DNS (con punteros de compresión).
func dnsName(b []byte, off int) (string, int, bool) {
	var parts []string
	end, jumped := -1, 0
	for {
		if off >= len(b) || jumped > 10 {
			return "", 0, false
		}
		l := int(b[off])
		switch {
		case l == 0:
			if end < 0 {
				end = off + 1
			}
			return strings.Join(parts, "."), end, true
		case l&0xC0 == 0xC0:
			if off+1 >= len(b) {
				return "", 0, false
			}
			if end < 0 {
				end = off + 2
			}
			off = int(binary.BigEndian.Uint16(b[off:off+2]) & 0x3FFF)
			jumped++
		default:
			if off+1+l > len(b) {
				return "", 0, false
			}
			parts = append(parts, string(b[off+1:off+1+l]))
			off += 1 + l
		}
	}
}

func cleanHostname(n string) string {
	n = strings.TrimSuffix(n, ".")
	for _, s := range []string{".local", ".lan", ".home", ".localdomain"} {
		n = strings.TrimSuffix(n, s)
	}
	return n
}

// discoverNames pregunta a la vez por NetBIOS y mDNS a los equipos indicados.
func discoverNames(ips []string) map[string]string {
	out := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)
	for _, ip := range ips {
		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			n := mdnsName(ip, 900*time.Millisecond)
			if n == "" {
				n = netbiosName(ip, 900*time.Millisecond)
			}
			if n != "" {
				mu.Lock()
				out[ip] = n
				mu.Unlock()
			}
		}(ip)
	}
	wg.Wait()
	return out
}

/* ---------- Fabricante por la MAC (lista OUI de la IEEE) ---------- */

type ouiDB struct {
	mu      sync.Mutex
	path    string
	cache   map[string]string // prefijo → fabricante, solo los que se han buscado
	fetched time.Time
}

const ouiURL = "https://standards-oui.ieee.org/oui/oui.csv"

// ensure descarga la lista si no existe o tiene más de 90 días. Ocupa unos
// 3 MB en disco y no se carga entera en memoria.
func (o *ouiDB) ensure() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if st, err := os.Stat(o.path); err == nil && time.Since(st.ModTime()) < 90*24*time.Hour {
		return
	}
	if time.Since(o.fetched) < 6*time.Hour {
		return
	}
	o.fetched = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ouiURL, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return
	}
	tmp := o.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	_, err = io.Copy(f, io.LimitReader(res.Body, 20<<20))
	f.Close()
	if err == nil {
		_ = os.Rename(tmp, o.path)
		o.cache = map[string]string{}
	}
}

// lookup devuelve el fabricante de varias MAC leyendo el archivo una vez.
func (o *ouiDB) lookup(macs []string) map[string]string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.cache == nil {
		o.cache = map[string]string{}
	}
	want := map[string]bool{}
	for _, m := range macs {
		p := ouiPrefix(m)
		if _, ok := o.cache[p]; !ok && p != "" {
			want[p] = true
		}
	}
	if len(want) > 0 {
		if f, err := os.Open(o.path); err == nil {
			sc := bufio.NewScanner(f)
			for sc.Scan() && len(want) > 0 {
				l := sc.Text()
				// MA-L,286FB9,"Nokia Shanghai Bell Co., Ltd.",...
				parts := strings.SplitN(l, ",", 3)
				if len(parts) < 3 || !want[parts[1]] {
					continue
				}
				name := parts[2]
				if strings.HasPrefix(name, `"`) {
					if i := strings.Index(name[1:], `"`); i >= 0 {
						name = name[1 : i+1]
					}
				} else if i := strings.Index(name, ","); i >= 0 {
					name = name[:i]
				}
				o.cache[parts[1]] = shortVendor(name)
				delete(want, parts[1])
			}
			f.Close()
		}
		for p := range want {
			o.cache[p] = ""
		}
	}
	out := map[string]string{}
	for _, m := range macs {
		if v := o.cache[ouiPrefix(m)]; v != "" {
			out[m] = v
		}
	}
	return out
}

func ouiPrefix(mac string) string {
	h := strings.ToUpper(strings.NewReplacer(":", "", "-", "").Replace(mac))
	if len(h) < 6 {
		return ""
	}
	return h[:6]
}

// shortVendor acorta «Apple, Inc.» a «Apple».
func shortVendor(s string) string {
	s = strings.TrimSpace(s)
	for _, suf := range []string{", Inc.", " Inc.", " Inc", ", Ltd.", " Co., Ltd.", " Co.,Ltd.", " Co.,Ltd", " Corporation", " Corp.", " Limited", " GmbH", " LLC", " Technologies", " Technology", " Electronics"} {
		if i := strings.Index(s, suf); i > 0 {
			s = s[:i]
		}
	}
	return strings.TrimRight(s, " ,.")
}

func newOUI(dataDir string) *ouiDB { return &ouiDB{path: filepath.Join(dataDir, "oui.csv")} }
