package core

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

/* Integración con el router. Cada modelo es un controlador con las mismas
 * operaciones; hoy hay dos:
 *  - OpenWrt, por ubus (JSON-RPC en /ubus, el mismo que usa LuCI);
 *  - MikroTik RouterOS 7, por su API REST (/rest).
 * Lo que da al panel: modelo y versión, los nombres que el router conoce por
 * DHCP, bloquear o desbloquear el acceso a Internet de un equipo por su MAC y
 * reiniciar el router. La contraseña vive en secrets/router.json. */

type RouterInfo struct {
	Model    string `json:"model"`
	Firmware string `json:"firmware"`
	Uptime   int64  `json:"uptimeSec"`
	Hostname string `json:"hostname"`
}

type RouterLease struct {
	MAC  string `json:"mac"`
	IP   string `json:"ip"`
	Name string `json:"name"`
}

type routerDriver interface {
	Info(ctx context.Context) (RouterInfo, error)
	Leases(ctx context.Context) ([]RouterLease, error)
	Blocked(ctx context.Context) ([]string, error)
	Block(ctx context.Context, mac string) error
	Unblock(ctx context.Context, mac string) error
	Reboot(ctx context.Context) error
}

type RouterState struct {
	Configured bool        `json:"configured"`
	Kind       string      `json:"kind"`
	Connected  bool        `json:"connected"`
	Error      string      `json:"error"`
	Info       *RouterInfo `json:"info"`
	Blocked    []string    `json:"blocked"`
	Leases     int         `json:"leases"`
	CheckedAt  *time.Time  `json:"checkedAt"`
	HasPass    bool        `json:"hasPassword"`
}

var RouterKinds = map[string]string{"openwrt": "OpenWrt", "mikrotik": "MikroTik RouterOS 7"}

type routerSecret struct {
	Password string `json:"password"`
}

type Router struct {
	mu    sync.Mutex
	st    RouterState
	sec   routerSecret
	f     jsonFile[routerSecret]
	names map[string]string
}

func newRouter(dataDir string) *Router {
	r := &Router{st: RouterState{Blocked: []string{}}, names: map[string]string{}}
	r.f.path = filepath.Join(dataDir, "secrets", "router.json")
	_ = r.f.Load(&r.sec)
	return r
}

func (r *Router) SetPassword(p *string) error {
	if p == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sec.Password = *p
	return r.f.Save(r.sec)
}

func (r *Router) State() RouterState {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.st
	s.Blocked = append([]string{}, r.st.Blocked...)
	s.HasPass = r.sec.Password != ""
	return s
}

func (r *Router) Names() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.names))
	for k, v := range r.names {
		out[k] = v
	}
	return out
}

func (r *Router) driver(c RouterConfig) (routerDriver, error) {
	r.mu.Lock()
	pass := r.sec.Password
	r.mu.Unlock()
	if c.Kind == "" {
		return nil, errors.New("El router no está configurado.")
	}
	if pass == "" {
		return nil, errors.New("Falta la contraseña del router.")
	}
	hc := &http.Client{Timeout: 12 * time.Second}
	if c.Insecure {
		// Muchos routers usan un certificado propio; el usuario lo acepta a sabiendas.
		hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	base := strings.TrimRight(c.URL, "/")
	switch c.Kind {
	case "openwrt":
		return &openwrt{base: base, user: c.User, pass: pass, hc: hc}, nil
	case "mikrotik":
		return &mikrotik{base: base, user: c.User, pass: pass, hc: hc}, nil
	}
	return nil, errors.New("Tipo de router desconocido.")
}

// Refresh lee el estado del router (cada minuto y con «Actualizar»).
func (r *Router) Refresh(c RouterConfig) {
	now := time.Now()
	st := RouterState{Configured: c.Kind != "", Kind: c.Kind, Blocked: []string{}, CheckedAt: &now}
	names := map[string]string{}
	if c.Kind != "" {
		if d, err := r.driver(c); err != nil {
			st.Error = err.Error()
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			info, err := d.Info(ctx)
			if err == nil {
				st.Connected, st.Info = true, &info
				if ls, e := d.Leases(ctx); e == nil {
					st.Leases = len(ls)
					for _, l := range ls {
						if l.Name != "" && l.Name != "*" {
							names[strings.ToUpper(l.MAC)] = l.Name
						}
					}
				}
				if b, e := d.Blocked(ctx); e == nil {
					st.Blocked = b
				}
			} else {
				st.Error = err.Error()
			}
			cancel()
		}
	}
	r.mu.Lock()
	r.st, r.names = st, names
	r.mu.Unlock()
}

// Test prueba una configuración antes de guardarla.
func (r *Router) Test(c RouterConfig, pass *string) (RouterInfo, error) {
	tmp := &Router{sec: r.sec}
	if pass != nil {
		tmp.sec.Password = *pass
	}
	d, err := tmp.driver(c)
	if err != nil {
		return RouterInfo{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return d.Info(ctx)
}

func (r *Router) Do(c RouterConfig, op, mac string) error {
	d, err := r.driver(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch op {
	case "block", "unblock":
		hw, err := net.ParseMAC(mac)
		if err != nil {
			return errors.New("MAC no válida.")
		}
		mac = strings.ToUpper(hw.String())
		if op == "block" {
			err = d.Block(ctx, mac)
		} else {
			err = d.Unblock(ctx, mac)
		}
		if err == nil {
			if b, e := d.Blocked(ctx); e == nil {
				r.mu.Lock()
				r.st.Blocked = b
				r.mu.Unlock()
			}
		}
		return err
	case "reboot":
		return d.Reboot(ctx)
	}
	return errors.New("Operación desconocida.")
}

func ValidRouter(c RouterConfig) error {
	if c.Kind == "" {
		return nil
	}
	if RouterKinds[c.Kind] == "" {
		return errors.New("Tipo de router desconocido.")
	}
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("La dirección del router debe ser como https://192.168.1.1.")
	}
	if strings.TrimSpace(c.User) == "" {
		return errors.New("Falta el usuario del router.")
	}
	return nil
}

func netErr(base string, err error) error {
	s := err.Error()
	switch {
	case strings.Contains(s, "certificate"):
		return errors.New("El certificado del router no es de confianza. Si es el suyo propio, marca «Aceptar el certificado del router».")
	case strings.Contains(s, "refused"):
		return errors.New("El router rechazó la conexión en " + base + ". Revisa la dirección y que su API esté activada.")
	case strings.Contains(s, "timeout"), strings.Contains(s, "deadline"):
		return errors.New("El router no responde en " + base + ".")
	}
	return errors.New("No se pudo conectar con el router: " + s)
}

/* ---------- OpenWrt (ubus) ---------- */

type openwrt struct {
	base, user, pass string
	hc               *http.Client
	sid              string
}

const ubusNull = "00000000000000000000000000000000"

func (o *openwrt) call(ctx context.Context, obj, method string, args any, out any) error {
	if o.sid == "" && !(obj == "session" && method == "login") {
		if err := o.login(ctx); err != nil {
			return err
		}
	}
	sid := o.sid
	if sid == "" {
		sid = ubusNull
	}
	if args == nil {
		args = map[string]any{}
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "call", "params": []any{sid, obj, method, args}})
	req, _ := http.NewRequestWithContext(ctx, "POST", o.base+"/ubus", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := o.hc.Do(req)
	if err != nil {
		return netErr(o.base, err)
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return errors.New("El router no tiene la API ubus en " + o.base + "/ubus. ¿Es OpenWrt con LuCI?")
	}
	var r struct {
		Result []json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&r); err != nil {
		return errors.New("Respuesta del router no válida.")
	}
	if r.Error != nil {
		return errors.New("El router respondió: " + r.Error.Message)
	}
	if len(r.Result) == 0 {
		return errors.New("Respuesta del router vacía.")
	}
	var code int
	_ = json.Unmarshal(r.Result[0], &code)
	if code != 0 {
		switch code {
		case 6:
			return errors.New("Permiso denegado en el router: usa el usuario root o uno con acceso completo.")
		case 4:
			return fmt.Errorf("El router no tiene %s.%s (instala luci-mod-rpc).", obj, method)
		}
		return fmt.Errorf("El router devolvió el código %d en %s.%s.", code, obj, method)
	}
	if out != nil && len(r.Result) > 1 {
		return json.Unmarshal(r.Result[1], out)
	}
	return nil
}

func (o *openwrt) login(ctx context.Context) error {
	var r struct {
		Session string `json:"ubus_rpc_session"`
	}
	err := o.call(ctx, "session", "login", map[string]string{"username": o.user, "password": o.pass}, &r)
	if err != nil {
		if strings.HasPrefix(err.Error(), "Permiso denegado") {
			return errors.New("Usuario o contraseña del router incorrectos.")
		}
		return err
	}
	if r.Session == "" {
		return errors.New("Usuario o contraseña del router incorrectos.")
	}
	o.sid = r.Session
	return nil
}

func (o *openwrt) Info(ctx context.Context) (RouterInfo, error) {
	var b struct {
		Model    string
		Hostname string
		Release  struct{ Description string }
	}
	if err := o.call(ctx, "system", "board", nil, &b); err != nil {
		return RouterInfo{}, err
	}
	var i struct{ Uptime int64 }
	_ = o.call(ctx, "system", "info", nil, &i)
	return RouterInfo{Model: b.Model, Firmware: b.Release.Description, Uptime: i.Uptime, Hostname: b.Hostname}, nil
}

func (o *openwrt) Leases(ctx context.Context) ([]RouterLease, error) {
	var r struct {
		Leases []struct {
			Hostname, Macaddr, Ipaddr string
		} `json:"dhcp_leases"`
	}
	if err := o.call(ctx, "luci-rpc", "getDHCPLeases", nil, &r); err != nil {
		return nil, err
	}
	out := make([]RouterLease, 0, len(r.Leases))
	for _, l := range r.Leases {
		out = append(out, RouterLease{MAC: strings.ToUpper(l.Macaddr), IP: l.Ipaddr, Name: l.Hostname})
	}
	return out, nil
}

const owPrefix = "umbral-bloqueo-"

func (o *openwrt) rules(ctx context.Context) (map[string]map[string]any, error) {
	var r struct {
		Values map[string]map[string]any `json:"values"`
	}
	if err := o.call(ctx, "uci", "get", map[string]string{"config": "firewall", "type": "rule"}, &r); err != nil {
		return nil, err
	}
	return r.Values, nil
}

func (o *openwrt) Blocked(ctx context.Context) ([]string, error) {
	rs, err := o.rules(ctx)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, v := range rs {
		if n, _ := v["name"].(string); strings.HasPrefix(n, owPrefix) {
			if m, _ := v["src_mac"].(string); m != "" {
				out = append(out, strings.ToUpper(m))
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func (o *openwrt) apply(ctx context.Context) error {
	if err := o.call(ctx, "uci", "commit", map[string]string{"config": "firewall"}, nil); err != nil {
		return err
	}
	// uci commit por ubus ya avisa a procd; recargar el cortafuegos lo hace inmediato.
	if err := o.call(ctx, "luci", "setInitAction", map[string]string{"name": "firewall", "action": "reload"}, nil); err != nil {
		_ = o.call(ctx, "file", "exec", map[string]any{"command": "/etc/init.d/firewall", "params": []string{"reload"}}, nil)
	}
	return nil
}

func (o *openwrt) Block(ctx context.Context, mac string) error {
	if b, err := o.Blocked(ctx); err == nil && contains(b, mac) {
		return nil
	}
	vals := map[string]any{"name": owPrefix + strings.ReplaceAll(mac, ":", ""), "src": "lan", "dest": "wan", "src_mac": mac, "proto": "all", "target": "REJECT"}
	if err := o.call(ctx, "uci", "add", map[string]any{"config": "firewall", "type": "rule", "values": vals}, nil); err != nil {
		return err
	}
	return o.apply(ctx)
}

func (o *openwrt) Unblock(ctx context.Context, mac string) error {
	rs, err := o.rules(ctx)
	if err != nil {
		return err
	}
	n := 0
	for sec, v := range rs {
		name, _ := v["name"].(string)
		m, _ := v["src_mac"].(string)
		if strings.HasPrefix(name, owPrefix) && strings.EqualFold(m, mac) {
			if err := o.call(ctx, "uci", "delete", map[string]string{"config": "firewall", "section": sec}, nil); err != nil {
				return err
			}
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return o.apply(ctx)
}

func (o *openwrt) Reboot(ctx context.Context) error {
	return o.call(ctx, "system", "reboot", nil, nil)
}

/* ---------- MikroTik RouterOS 7 (REST) ---------- */

type mikrotik struct {
	base, user, pass string
	hc               *http.Client
}

const mtComment = "umbral-bloqueo"

func (m *mikrotik) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(ctx, method, m.base+"/rest"+path, rd)
	req.SetBasicAuth(m.user, m.pass)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := m.hc.Do(req)
	if err != nil {
		return netErr(m.base, err)
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == 401:
		return errors.New("Usuario o contraseña del router incorrectos.")
	case res.StatusCode == 404 && path == "/system/resource":
		return errors.New("El router no tiene la API REST en " + m.base + "/rest. Necesita RouterOS 7 con el servicio www o www-ssl activo.")
	case res.StatusCode >= 300:
		var e struct{ Message, Detail string }
		_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&e)
		return errors.New("El router respondió: " + strings.TrimSpace(e.Message+" "+e.Detail))
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out)
	}
	return nil
}

func (m *mikrotik) Info(ctx context.Context) (RouterInfo, error) {
	var r map[string]string
	if err := m.do(ctx, "GET", "/system/resource", nil, &r); err != nil {
		return RouterInfo{}, err
	}
	var id map[string]string
	_ = m.do(ctx, "GET", "/system/identity", nil, &id)
	return RouterInfo{Model: r["board-name"], Firmware: "RouterOS " + r["version"], Uptime: parseROSDuration(r["uptime"]), Hostname: id["name"]}, nil
}

// parseROSDuration convierte «1w2d3h4m5s» en segundos.
func parseROSDuration(s string) int64 {
	var total, n int64
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			n = n*10 + int64(c-'0')
		default:
			total += n * map[rune]int64{'w': 604800, 'd': 86400, 'h': 3600, 'm': 60, 's': 1}[c]
			n = 0
		}
	}
	return total
}

func (m *mikrotik) Leases(ctx context.Context) ([]RouterLease, error) {
	var r []map[string]string
	if err := m.do(ctx, "GET", "/ip/dhcp-server/lease", nil, &r); err != nil {
		return nil, err
	}
	out := make([]RouterLease, 0, len(r))
	for _, l := range r {
		name := l["host-name"]
		if c := l["comment"]; c != "" {
			name = c
		}
		out = append(out, RouterLease{MAC: strings.ToUpper(l["mac-address"]), IP: l["address"], Name: name})
	}
	return out, nil
}

func (m *mikrotik) filters(ctx context.Context) ([]map[string]string, error) {
	var r []map[string]string
	err := m.do(ctx, "GET", "/ip/firewall/filter", nil, &r)
	return r, err
}

func (m *mikrotik) Blocked(ctx context.Context) ([]string, error) {
	fs, err := m.filters(ctx)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, f := range fs {
		if f["comment"] == mtComment && f["src-mac-address"] != "" {
			out = append(out, strings.ToUpper(f["src-mac-address"]))
		}
	}
	sort.Strings(out)
	return out, nil
}

func (m *mikrotik) Block(ctx context.Context, mac string) error {
	fs, err := m.filters(ctx)
	if err != nil {
		return err
	}
	rule := map[string]string{"chain": "forward", "src-mac-address": mac, "action": "drop", "comment": mtComment}
	for _, f := range fs {
		if f["comment"] == mtComment && strings.EqualFold(f["src-mac-address"], mac) {
			return nil
		}
	}
	// La regla va la primera de la cadena forward para que nada la adelante.
	for _, f := range fs {
		if f["chain"] == "forward" && f[".id"] != "" {
			rule["place-before"] = f[".id"]
			break
		}
	}
	return m.do(ctx, "PUT", "/ip/firewall/filter", rule, nil)
}

func (m *mikrotik) Unblock(ctx context.Context, mac string) error {
	fs, err := m.filters(ctx)
	if err != nil {
		return err
	}
	for _, f := range fs {
		if f["comment"] == mtComment && strings.EqualFold(f["src-mac-address"], mac) {
			if err := m.do(ctx, "DELETE", "/ip/firewall/filter/"+url.PathEscape(f[".id"]), nil, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *mikrotik) Reboot(ctx context.Context) error {
	err := m.do(ctx, "POST", "/system/reboot", map[string]string{}, nil)
	// El router corta la conexión al reiniciarse: no es un error.
	if err != nil && strings.Contains(err.Error(), "No se pudo conectar") {
		return nil
	}
	return err
}
