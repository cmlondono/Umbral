package core

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// testServer levanta un servidor HTTP de verdad en 127.0.0.1 para las pruebas.
type testServer struct {
	URL string
	l   net.Listener
}

func newTestServer(h http.Handler) *testServer {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	go func() { _ = http.Serve(l, h) }()
	return &testServer{URL: "http://" + l.Addr().String(), l: l}
}

func (s *testServer) Close()               { _ = s.l.Close() }
func (s *testServer) Client() *http.Client { return &http.Client{} }

// Simulador mínimo de ubus de OpenWrt: sesión, board, info, leases y uci.
func fakeOpenWrt(t *testing.T) (*testServer, *map[string]map[string]any) {
	var mu sync.Mutex
	rules := map[string]map[string]any{"cfg01": {"name": "Allow-Ping", "src": "wan"}}
	n := 0
	srv := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ubus" {
			http.NotFound(w, r)
			return
		}
		var req struct{ Params []json.RawMessage }
		_ = json.NewDecoder(r.Body).Decode(&req)
		var sid, obj, method string
		_ = json.Unmarshal(req.Params[0], &sid)
		_ = json.Unmarshal(req.Params[1], &obj)
		_ = json.Unmarshal(req.Params[2], &method)
		reply := func(res ...any) {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": res})
		}
		if obj == "session" && method == "login" {
			var a map[string]string
			_ = json.Unmarshal(req.Params[3], &a)
			if a["password"] != "secreta" {
				reply(6)
				return
			}
			reply(0, map[string]string{"ubus_rpc_session": "abc"})
			return
		}
		if sid != "abc" {
			reply(6)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch obj + "." + method {
		case "system.board":
			reply(0, map[string]any{"model": "GL.iNet GL-MT6000", "hostname": "OpenWrt", "release": map[string]string{"description": "OpenWrt 24.10.0"}})
		case "system.info":
			reply(0, map[string]any{"uptime": 3600})
		case "luci-rpc.getDHCPLeases":
			reply(0, map[string]any{"dhcp_leases": []map[string]string{{"hostname": "portatil", "macaddr": "aa:bb:cc:dd:ee:01", "ipaddr": "192.168.1.50"}}})
		case "uci.get":
			reply(0, map[string]any{"values": rules})
		case "uci.add":
			var a struct{ Values map[string]any }
			_ = json.Unmarshal(req.Params[3], &a)
			n++
			rules["cfgx"+string(rune('0'+n))] = a.Values
			reply(0, map[string]string{"section": "cfgx"})
		case "uci.delete":
			var a map[string]string
			_ = json.Unmarshal(req.Params[3], &a)
			delete(rules, a["section"])
			reply(0)
		case "uci.commit", "luci.setInitAction", "system.reboot":
			reply(0)
		default:
			reply(4)
		}
	}))
	return srv, &rules
}

func TestOpenWrtDriver(t *testing.T) {
	srv, rules := fakeOpenWrt(t)
	defer srv.Close()
	ctx := context.Background()
	bad := &openwrt{base: srv.URL, user: "root", pass: "mala", hc: srv.Client()}
	if _, err := bad.Info(ctx); err == nil || !strings.Contains(err.Error(), "incorrectos") {
		t.Fatalf("contraseña mala: %v", err)
	}
	o := &openwrt{base: srv.URL, user: "root", pass: "secreta", hc: srv.Client()}
	info, err := o.Info(ctx)
	if err != nil || info.Model != "GL.iNet GL-MT6000" || info.Uptime != 3600 {
		t.Fatalf("info = %+v, %v", info, err)
	}
	ls, err := o.Leases(ctx)
	if err != nil || len(ls) != 1 || ls[0].MAC != "AA:BB:CC:DD:EE:01" || ls[0].Name != "portatil" {
		t.Fatalf("leases = %+v, %v", ls, err)
	}
	if err := o.Block(ctx, "AA:BB:CC:DD:EE:01"); err != nil {
		t.Fatal(err)
	}
	_ = o.Block(ctx, "AA:BB:CC:DD:EE:01") // no duplica
	if b, _ := o.Blocked(ctx); len(b) != 1 || b[0] != "AA:BB:CC:DD:EE:01" || len(*rules) != 2 {
		t.Fatalf("bloqueados = %v, reglas = %v", b, *rules)
	}
	if err := o.Unblock(ctx, "aa:bb:cc:dd:ee:01"); err != nil {
		t.Fatal(err)
	}
	if b, _ := o.Blocked(ctx); len(b) != 0 || len(*rules) != 1 {
		t.Fatalf("tras desbloquear = %v", b)
	}
}

// Simulador de la API REST de RouterOS 7.
func TestMikrotikDriver(t *testing.T) {
	var mu sync.Mutex
	filters := []map[string]string{{".id": "*1", "chain": "forward", "action": "fasttrack-connection"}}
	next := 2
	srv := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		if u != "admin" || p != "secreta" {
			w.WriteHeader(401)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /rest/system/resource":
			_ = json.NewEncoder(w).Encode(map[string]string{"board-name": "hAP ax2", "version": "7.16.1 (stable)", "uptime": "1d2h3m4s"})
		case "GET /rest/system/identity":
			_ = json.NewEncoder(w).Encode(map[string]string{"name": "MikroTik"})
		case "GET /rest/ip/dhcp-server/lease":
			_ = json.NewEncoder(w).Encode([]map[string]string{{"mac-address": "AA:BB:CC:DD:EE:02", "address": "192.168.88.20", "host-name": "tele"}})
		case "GET /rest/ip/firewall/filter":
			_ = json.NewEncoder(w).Encode(filters)
		case "PUT /rest/ip/firewall/filter":
			var f map[string]string
			_ = json.NewDecoder(r.Body).Decode(&f)
			if f["place-before"] != "*1" {
				t.Errorf("la regla debería ir la primera: %v", f)
			}
			delete(f, "place-before")
			f[".id"] = "*" + string(rune('0'+next))
			next++
			filters = append([]map[string]string{f}, filters...)
			_ = json.NewEncoder(w).Encode(f)
		case "DELETE /rest/ip/firewall/filter/*2":
			filters = filters[1:]
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	m := &mikrotik{base: srv.URL, user: "admin", pass: "secreta", hc: srv.Client()}
	info, err := m.Info(ctx)
	if err != nil || info.Model != "hAP ax2" || info.Uptime != 93784 || info.Hostname != "MikroTik" {
		t.Fatalf("info = %+v, %v", info, err)
	}
	if ls, _ := m.Leases(ctx); len(ls) != 1 || ls[0].Name != "tele" {
		t.Fatalf("leases = %+v", ls)
	}
	if err := m.Block(ctx, "AA:BB:CC:DD:EE:02"); err != nil {
		t.Fatal(err)
	}
	if b, _ := m.Blocked(ctx); len(b) != 1 {
		t.Fatalf("bloqueados = %v", b)
	}
	if err := m.Unblock(ctx, "AA:BB:CC:DD:EE:02"); err != nil {
		t.Fatal(err)
	}
	if b, _ := m.Blocked(ctx); len(b) != 0 {
		t.Fatalf("tras desbloquear = %v", b)
	}
	bad := &mikrotik{base: srv.URL, user: "admin", pass: "x", hc: srv.Client()}
	if _, err := bad.Info(ctx); err == nil || !strings.Contains(err.Error(), "incorrectos") {
		t.Fatalf("contraseña mala: %v", err)
	}
}

func TestParseServeStatus(t *testing.T) {
	b := []byte(`{"TCP":{"443":{"HTTPS":true},"8080":{"HTTPS":true}},"Web":{"srv.tail1234.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:8420"}}},"srv.tail1234.ts.net:8080":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:8080"}}}}}`)
	l := parseServeStatus(b)
	if len(l) != 2 || l[0].Port != 443 || l[0].URL != "https://srv.tail1234.ts.net" || l[0].Local != 8420 || l[1].URL != "https://srv.tail1234.ts.net:8080" || l[1].Local != 8080 {
		t.Fatalf("serve = %+v", l)
	}
	if l := parseServeStatus([]byte(`{}`)); len(l) != 0 {
		t.Fatalf("vacío = %+v", l)
	}
}

func TestClientIPBehindProxy(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 100.88.1.2")
	if ip := clientIP(r).String(); ip != "100.88.1.2" {
		t.Fatalf("detrás del proxy = %s", ip)
	}
	r.RemoteAddr = "192.168.1.30:5555"
	if ip := clientIP(r).String(); ip != "192.168.1.30" {
		t.Fatalf("sin proxy, la cabecera no cuenta: %s", ip)
	}
}

func TestValidRouter(t *testing.T) {
	if ValidRouter(RouterConfig{}) != nil || ValidRouter(RouterConfig{Kind: "openwrt", URL: "https://192.168.1.1", User: "root"}) != nil {
		t.Fatal("configuraciones válidas rechazadas")
	}
	if ValidRouter(RouterConfig{Kind: "tplink", URL: "https://x", User: "a"}) == nil || ValidRouter(RouterConfig{Kind: "openwrt", URL: "192.168.1.1", User: "root"}) == nil {
		t.Fatal("configuraciones no válidas aceptadas")
	}
}
