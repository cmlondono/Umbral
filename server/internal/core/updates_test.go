package core

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

/* ---------- Simulador mínimo del motor de Docker ---------- */

type fakeCtr struct {
	id, name, ref, image string
	running              bool
	host                 map[string]any
	ep, cmd              []string
	project              string
	mounts               []map[string]any
}

type fakeImg struct{ ep, cmd []string }

type fakeDocker struct {
	mu     sync.Mutex
	ctrs   map[string]*fakeCtr // por nombre
	images map[string]string   // referencia → ID
	cfgs   map[string]fakeImg  // ID → entrypoint y command de la imagen
	pulls  map[string]string   // referencia → ID tras descargarla
	broken map[string]bool     // imágenes con las que un contenedor no arranca
	crashy map[string]bool     // imágenes con las que el contenedor se reinicia sin parar
	maint  bool                // modo de mantenimiento de Nextcloud
	events []string
	execs  map[string][]string
	n      int
}

func newFake() *fakeDocker {
	return &fakeDocker{ctrs: map[string]*fakeCtr{}, images: map[string]string{}, cfgs: map[string]fakeImg{}, pulls: map[string]string{},
		broken: map[string]bool{}, crashy: map[string]bool{}, execs: map[string][]string{}}
}

// add crea un contenedor con la imagen que tiene ahora su etiqueta. ep y cmd
// nulos: los de la imagen.
func (f *fakeDocker) add(name, ref, project string, ep []string, vols ...string) *fakeCtr {
	f.n++
	img := f.images[ref]
	c := &fakeCtr{id: fmt.Sprintf("%064d", f.n), name: name, ref: ref, image: img, running: true, project: project,
		host: map[string]any{"NetworkMode": "bridge", "RestartPolicy": map[string]any{"Name": "always"}}, ep: f.cfgs[img].ep, cmd: f.cfgs[img].cmd}
	if ep != nil {
		c.ep, c.cmd = ep, nil
	}
	for _, v := range vols {
		if strings.HasPrefix(v, "/") {
			c.mounts = append(c.mounts, map[string]any{"Type": "bind", "Source": v})
		} else {
			c.mounts = append(c.mounts, map[string]any{"Type": "volume", "Name": v})
		}
	}
	f.ctrs[name] = c
	return c
}

func (f *fakeDocker) find(key string) *fakeCtr {
	if c, ok := f.ctrs[key]; ok {
		return c
	}
	for _, c := range f.ctrs {
		if c.id == key {
			return c
		}
	}
	return nil
}

func (f *fakeDocker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := r.URL.Path
	reply := func(code int, v any) {
		w.WriteHeader(code)
		if v != nil {
			_ = json.NewEncoder(w).Encode(v)
		}
	}
	switch {
	case r.Method == "GET" && p == "/containers/json":
		var out []map[string]any
		for _, c := range f.ctrs {
			img := c.ref
			if f.images[c.ref] != c.image {
				img = c.image // Docker muestra el ID si la etiqueta ya apunta a otra imagen
			}
			st := "exited"
			if c.running {
				st = "running"
			}
			out = append(out, map[string]any{"Id": c.id, "Names": []string{"/" + c.name}, "Image": img, "ImageID": c.image, "State": st,
				"Labels": map[string]string{"com.docker.compose.project": c.project}})
		}
		reply(200, out)
	case r.Method == "POST" && p == "/containers/create":
		var cfg struct {
			Image           string
			Entrypoint, Cmd []string
			Labels          map[string]string
			HostConfig      map[string]any
		}
		_ = json.NewDecoder(r.Body).Decode(&cfg)
		name := r.URL.Query().Get("name")
		if f.ctrs[name] != nil {
			reply(409, map[string]string{"message": "el nombre ya existe"})
			return
		}
		img := cfg.Image
		if !strings.HasPrefix(img, "sha256:") {
			img = f.images[cfg.Image]
		}
		if img == "" {
			reply(404, map[string]string{"message": "no existe la imagen"})
			return
		}
		f.n++
		old := f.ctrs[name+"-anterior"]
		c := &fakeCtr{id: fmt.Sprintf("%064d", f.n+1000), name: name, ref: cfg.Image, image: img, host: cfg.HostConfig, ep: cfg.Entrypoint, cmd: cfg.Cmd, project: cfg.Labels["com.docker.compose.project"]}
		if old != nil {
			c.mounts, c.project = old.mounts, old.project
		}
		f.ctrs[name] = c
		f.events = append(f.events, "create "+name)
		reply(201, map[string]string{"Id": c.id})
	case r.Method == "POST" && p == "/images/create":
		ref := r.URL.Query().Get("fromImage") + ":" + r.URL.Query().Get("tag")
		if id, ok := f.pulls[ref]; ok {
			f.cfgs[id] = f.cfgs[f.images[ref]]
			f.images[ref] = id
		}
		f.events = append(f.events, "pull "+ref)
		reply(200, map[string]string{"status": "Descargado"})
	case strings.HasPrefix(p, "/images/") && strings.HasSuffix(p, "/json"):
		ref := strings.TrimSuffix(strings.TrimPrefix(p, "/images/"), "/json")
		id := f.images[ref]
		if strings.HasPrefix(ref, "sha256:") {
			id = ref
		}
		if id == "" {
			reply(404, map[string]string{"message": "no existe"})
			return
		}
		reply(200, map[string]any{"Id": id, "RepoDigests": []string{"x@sha256:abc"}, "Config": map[string]any{"Env": []string{}, "Entrypoint": f.cfgs[id].ep, "Cmd": f.cfgs[id].cmd}})
	case strings.HasPrefix(p, "/exec/"):
		id, action, _ := strings.Cut(strings.TrimPrefix(p, "/exec/"), "/")
		if action == "json" {
			reply(200, map[string]int{"ExitCode": 0})
			return
		}
		cmd := strings.Join(f.execs[id], " ")
		out := ""
		switch {
		case strings.Contains(cmd, "maintenance:mode"):
			f.maint = strings.HasSuffix(cmd, "--on")
			f.events = append(f.events, "occ "+cmd[strings.LastIndex(cmd, " ")+1:])
		case strings.Contains(cmd, "status"):
			b, _ := json.Marshal(map[string]any{"installed": true, "versionstring": "35.0.3", "maintenance": f.maint, "needsDbUpgrade": false})
			out = string(b)
		default: // copia de la base de datos
			out = strings.Repeat("x", 2000)
			f.events = append(f.events, "backup")
		}
		w.WriteHeader(200)
		hdr := make([]byte, 8)
		hdr[0] = 1
		binary.BigEndian.PutUint32(hdr[4:], uint32(len(out)))
		_, _ = w.Write(append(hdr, out...))
	case strings.HasPrefix(p, "/containers/"):
		key, action, _ := strings.Cut(strings.TrimPrefix(p, "/containers/"), "/")
		c := f.find(key)
		if c == nil {
			reply(404, map[string]string{"message": "no existe el contenedor " + key})
			return
		}
		switch {
		case r.Method == "DELETE":
			delete(f.ctrs, c.name)
			f.events = append(f.events, "remove "+c.name)
			reply(204, nil)
		case action == "json":
			restarts := 0
			if f.crashy[c.image] {
				restarts = 5
			}
			reply(200, map[string]any{"Id": c.id, "Image": c.image, "RestartCount": restarts,
				"Config":     map[string]any{"Image": c.ref, "Entrypoint": c.ep, "Cmd": c.cmd, "Labels": map[string]string{"com.docker.compose.project": c.project}},
				"HostConfig": c.host, "State": map[string]any{"Running": c.running}, "Mounts": c.mounts, "NetworkSettings": map[string]any{"Networks": map[string]any{}}})
		case action == "exec":
			var body struct{ Cmd []string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			id := fmt.Sprintf("e%d", len(f.execs))
			f.execs[id] = body.Cmd
			reply(201, map[string]string{"Id": id})
		case action == "update":
			var body struct{ RestartPolicy map[string]any }
			_ = json.NewDecoder(r.Body).Decode(&body)
			c.host["RestartPolicy"] = body.RestartPolicy
			reply(200, map[string]any{"Warnings": []string{}})
		case action == "stop":
			if !c.running {
				reply(304, nil)
				return
			}
			c.running = false
			f.events = append(f.events, "stop "+c.name)
			reply(204, nil)
		case action == "start":
			c.running = !f.broken[c.image]
			f.events = append(f.events, "start "+c.name)
			reply(204, nil)
		case action == "rename":
			nn := r.URL.Query().Get("name")
			delete(f.ctrs, c.name)
			c.name = nn
			f.ctrs[nn] = c
			reply(204, nil)
		default:
			reply(404, nil)
		}
	default:
		reply(404, nil)
	}
}

// before: el evento a ocurre antes que b.
func (f *fakeDocker) before(t *testing.T, a, b string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	i, j := slices.Index(f.events, a), slices.Index(f.events, b)
	if i < 0 || j < 0 || i > j {
		t.Errorf("se esperaba «%s» antes de «%s»: %v", a, b, f.events)
	}
}

func (f *fakeDocker) has(ev string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.events, ev)
}

func fakeDockerClient(addr string) *Docker {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}}
	return &Docker{hc: &http.Client{Transport: tr, Timeout: 5 * time.Second}, stream: &http.Client{Transport: tr}}
}

// testApp: un App con el Docker simulado y la lista de contenedores cargada.
func testApp(t *testing.T, f *fakeDocker) *App {
	t.Helper()
	pollEvery, ncPollEvery = time.Millisecond, time.Millisecond
	t.Cleanup(func() { pollEvery, ncPollEvery = 2*time.Second, 5*time.Second })
	srv := newTestServer(f)
	t.Cleanup(srv.Close)
	a, err := NewApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := a.Cfg.Public()
	p.DataRoot = t.TempDir()
	if err := a.Cfg.Update(p); err != nil {
		t.Fatal(err)
	}
	a.Docker = fakeDockerClient(strings.TrimPrefix(srv.URL, "http://"))
	reload(t, a)
	return a
}

func reload(t *testing.T, a *App) {
	t.Helper()
	cs, err := a.Docker.Containers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.docker = DockerState{Available: true, Containers: cs}
	a.mu.Unlock()
}

func waitJob(t *testing.T, a *App, id string) JobView {
	t.Helper()
	for i := 0; i < 1000; i++ {
		if j, ok := a.Job(id); ok && j.State != "running" {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("el trabajo no termina")
	return JobView{}
}

// servidor como el de los casos reales: Nextcloud con cron, MariaDB con un
// respaldo, Redis, otra pila con Postgres y AdGuard suelto.
func homeServer() *fakeDocker {
	f := newFake()
	set := func(ref, id string, ep, cmd []string) { f.images[ref], f.cfgs[id] = id, fakeImg{ep, cmd} }
	set("nextcloud:latest", "sha256:nc1", []string{"/entrypoint.sh"}, []string{"apache2-foreground"})
	set("mariadb:11", "sha256:mdb1", []string{"docker-entrypoint.sh"}, []string{"mariadbd"})
	set("redis:7", "sha256:redis1", []string{"docker-entrypoint.sh"}, []string{"redis-server"})
	set("postgres:17-alpine", "sha256:pg17", []string{"docker-entrypoint.sh"}, []string{"postgres"})
	set("postgres:16", "sha256:pg16", []string{"docker-entrypoint.sh"}, []string{"postgres"})
	set("adguard/adguardhome:latest", "sha256:ag1", []string{"/opt/adguardhome/AdGuardHome"}, nil)
	f.pulls = map[string]string{"nextcloud:latest": "sha256:nc2", "mariadb:11": "sha256:mdb2", "postgres:17-alpine": "sha256:pg17b", "adguard/adguardhome:latest": "sha256:ag2"}
	f.add("nextcloud-app", "nextcloud:latest", "nextcloud", nil, "nextcloud_data", "/datos/nextcloud")
	f.add("nextcloud-cron", "nextcloud:latest", "nextcloud", []string{"/cron.sh"}, "nextcloud_data", "/datos/nextcloud")
	f.add("nextcloud-db", "mariadb:11", "nextcloud", nil, "nextcloud_db")
	f.add("nextcloud-respaldo", "mariadb:11", "nextcloud", []string{"bash", "-c", "while true; do mariadb-dump; sleep 86400; done"}, "/srv/respaldos")
	f.add("nextcloud-redis", "redis:7", "nextcloud", nil)
	f.add("notas-db", "postgres:17-alpine", "notas", nil, "notas_db")
	f.add("notas-respaldo-1", "postgres:17-alpine", "notas", []string{"sh", "-c", "while true; do pg_dump; sleep 86400; done"})
	f.add("otro-pg", "postgres:16", "", nil, "otro_pg")
	f.add("adguard-home", "adguard/adguardhome:latest", "", nil, "/opt/adguard")
	return f
}

func (f *fakeDocker) imageOf(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c := f.ctrs[name]; c != nil {
		return c.image
	}
	return ""
}

func (f *fakeDocker) idOf(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c := f.ctrs[name]; c != nil {
		return c.id
	}
	return ""
}

func (f *fakeDocker) noLeftovers(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for n := range f.ctrs {
		if strings.HasSuffix(n, "-anterior") {
			t.Errorf("queda %s", n)
		}
	}
}

// infosOf clasifica un grupo leyendo el Docker simulado.
func infosOf(t *testing.T, a *App, ref string) []ctrInfo {
	t.Helper()
	var cs []Container
	for _, c := range a.docker.Containers {
		if normRef(c.Image) == ref {
			cs = append(cs, c)
		}
	}
	infos, err := a.inspectAll(context.Background(), cs)
	if err != nil {
		t.Fatal(err)
	}
	return infos
}

/* ---------- Pruebas ---------- */

func TestSharesWithAndRetarget(t *testing.T) {
	webID := strings.Repeat("a", 64)
	group := []Container{{Name: "web", ID: webID}}
	raw := func(mode string) map[string]json.RawMessage {
		b, _ := json.Marshal(map[string]any{"NetworkMode": mode, "Memory": 1073741824})
		return map[string]json.RawMessage{"HostConfig": b}
	}
	for mode, want := range map[string]bool{"container:web": true, "container:" + webID: true, "container:" + webID[:12]: true,
		"container:otro": false, "bridge": false, "container:aaa": false} {
		if got := sharesWith(raw(mode), group); got != want {
			t.Errorf("sharesWith(%q) = %v", mode, got)
		}
	}
	out := retarget(raw("container:"+webID), map[string]string{webID: "nuevo"})
	var host struct {
		NetworkMode string
		Memory      int64
	}
	_ = json.Unmarshal(out["HostConfig"], &host)
	if host.NetworkMode != "container:nuevo" || host.Memory != 1073741824 {
		t.Errorf("retarget: %+v", host)
	}
	// Por nombre no cambia: el contenedor nuevo se llama igual.
	_ = json.Unmarshal(retarget(raw("container:web"), map[string]string{webID: "nuevo"})["HostConfig"], &host)
	if host.NetworkMode != "container:web" {
		t.Errorf("retarget por nombre: %q", host.NetworkMode)
	}
}

func TestRefHelpers(t *testing.T) {
	for in, want := range map[string]string{"nextcloud": "nextcloud:latest", "nextcloud:30-apache": "nextcloud:30-apache",
		"registro:5000/app": "registro:5000/app:latest", "app@sha256:abc": "app@sha256:abc"} {
		if got := normRef(in); got != want {
			t.Errorf("normRef(%q) = %q", in, got)
		}
	}
	if s := joinList([]string{"a", "b", "c"}); s != "a, b y c" {
		t.Errorf("joinList = %q", s)
	}
}

// Papeles sin nombres: vonotas compartido, mismo proyecto con otro
// entrypoint, otro proyecto e instancias separadas de Nextcloud.
func TestClassification(t *testing.T) {
	f := homeServer()
	f.add("notas2-respaldo", "postgres:17-alpine", "notas2", []string{"sh", "-c", "pg_dump"})
	f.add("nc2-app", "nextcloud:latest", "nc2", nil, "nc2_data")
	f.add("nc2-cron", "nextcloud:latest", "nc2", []string{"/cron.sh"}, "nc2_data")
	a := testApp(t, f)

	nc := infosOf(t, a, "nextcloud:latest")
	units := unitsOf(nc, true)
	if len(units) != 2 {
		t.Fatalf("dos Nextcloud deben ser dos unidades: %d", len(units))
	}
	for _, u := range units {
		if len(u) != 2 || !u[0].own || u[1].own || roleOf(u[0], u[1]) != "aux" || u[0].project != u[1].project {
			t.Errorf("unidad mal formada: %s + %d", u[0].c.Name, len(u)-1)
		}
	}

	mdb := infosOf(t, a, "mariadb:11")
	p := pickPrincipal(mdb, "nextcloud-respaldo")
	if mdb[p].c.Name != "nextcloud-db" {
		t.Errorf("al pulsar en el respaldo, el principal debe ser la base de datos: %s", mdb[p].c.Name)
	}

	pg := infosOf(t, a, "postgres:17-alpine")
	pi := pickPrincipal(pg, "notas-db")
	for _, x := range pg {
		want := map[string]string{"notas-respaldo-1": "aux", "notas2-respaldo": "independent"}[x.c.Name]
		if x.c.Name != "notas-db" && roleOf(pg[pi], x) != want {
			t.Errorf("%s: papel %s, se esperaba %s", x.c.Name, roleOf(pg[pi], x), want)
		}
	}
	if len(infosOf(t, a, "postgres:16")) != 1 {
		t.Error("postgres:16 no debe mezclarse con postgres:17-alpine")
	}
}

// Un contenedor cuya etiqueta ya apunta a otra imagen aparece con su
// referencia original, no con el ID.
func TestContainersKeepRef(t *testing.T) {
	f := newFake()
	f.images["web:1"] = "sha256:viejo"
	f.add("cron", "web:1", "", nil)
	f.images["web:1"] = "sha256:nuevo"
	srv := newTestServer(f)
	defer srv.Close()
	cs, err := fakeDockerClient(strings.TrimPrefix(srv.URL, "http://")).Containers(context.Background())
	if err != nil || len(cs) != 1 {
		t.Fatal(err)
	}
	if cs[0].Image != "web:1" || cs[0].ImageID != "sha256:viejo" {
		t.Errorf("contenedor: %+v", cs[0])
	}
}

func TestUpdateNextcloudWithCron(t *testing.T) {
	f := homeServer()
	a := testApp(t, f)
	untouched := map[string]string{"nextcloud-db": f.idOf("nextcloud-db"), "nextcloud-redis": f.idOf("nextcloud-redis"), "nextcloud-respaldo": f.idOf("nextcloud-respaldo")}
	a.updates.Items = []Update{{Container: "nextcloud-app", Image: "nextcloud:latest", State: "available", Siblings: []Sibling{{Name: "nextcloud-cron", Role: "aux"}}}}
	j, err := a.ApplyUpdate("prueba", "nextcloud-app", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if end := waitJob(t, a, j.ID); end.State != "done" {
		t.Fatalf("terminó en %s: %s", end.State, end.Message)
	}
	if f.imageOf("nextcloud-app") != "sha256:nc2" || f.imageOf("nextcloud-cron") != "sha256:nc2" {
		t.Errorf("app y cron deben acabar con la misma imagen nueva: %s, %s", f.imageOf("nextcloud-app"), f.imageOf("nextcloud-cron"))
	}
	f.before(t, "occ --on", "stop nextcloud-cron")
	f.before(t, "backup", "stop nextcloud-cron")
	f.before(t, "stop nextcloud-cron", "create nextcloud-app")
	f.before(t, "create nextcloud-app", "create nextcloud-cron")
	f.before(t, "create nextcloud-cron", "occ --off")
	for n, id := range untouched {
		if f.idOf(n) != id {
			t.Errorf("%s no usa esa imagen y no debía recrearse", n)
		}
	}
	f.noLeftovers(t)
	u, _ := a.findUpdate("nextcloud-app")
	if u.State != "updated" || u.PreviousAlso["nextcloud-cron"] != "sha256:nc1" {
		t.Errorf("estado: %+v", u)
	}
}

func TestUpdateMariaDBFromBackupContainer(t *testing.T) {
	f := homeServer()
	a := testApp(t, f)
	untouched := map[string]string{"nextcloud-app": f.idOf("nextcloud-app"), "nextcloud-cron": f.idOf("nextcloud-cron"), "nextcloud-redis": f.idOf("nextcloud-redis")}
	a.updates.Items = []Update{{Container: "nextcloud-db", Image: "mariadb:11", State: "available", Siblings: []Sibling{{Name: "nextcloud-respaldo", Role: "aux"}}}}
	// Se pulsa en el respaldo: el principal es la base de datos.
	j, err := a.ApplyUpdate("prueba", "nextcloud-respaldo", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(j.copy().Steps[3].Label, "nextcloud-db") {
		t.Errorf("el principal debe ser nextcloud-db: %+v", j.copy().Steps)
	}
	if end := waitJob(t, a, j.ID); end.State != "done" {
		t.Fatalf("terminó en %s: %s", end.State, end.Message)
	}
	f.before(t, "stop nextcloud-respaldo", "create nextcloud-db")
	f.before(t, "create nextcloud-db", "create nextcloud-respaldo")
	if f.imageOf("nextcloud-db") != "sha256:mdb2" || f.imageOf("nextcloud-respaldo") != "sha256:mdb2" {
		t.Error("db y respaldo deben usar la imagen nueva")
	}
	for n, id := range untouched {
		if f.idOf(n) != id {
			t.Errorf("%s es del mismo proyecto pero no de esa imagen: no debía recrearse", n)
		}
	}
	if f.has("occ --on") {
		t.Error("MariaDB no usa el modo de mantenimiento de Nextcloud")
	}
	f.noLeftovers(t)
}

func TestUpdateSingleInstance(t *testing.T) {
	f := homeServer()
	a := testApp(t, f)
	a.updates.Items = []Update{{Container: "adguard-home", Image: "adguard/adguardhome:latest", State: "available"}}
	j, err := a.ApplyUpdate("prueba", "adguard-home", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(j.copy().Steps); n != 4 {
		t.Errorf("una sola instancia: descarga, compatibilidad, recrear y comprobar (%d pasos)", n)
	}
	if end := waitJob(t, a, j.ID); end.State != "done" {
		t.Fatalf("terminó en %s: %s", end.State, end.Message)
	}
	if f.imageOf("adguard-home") != "sha256:ag2" {
		t.Error("adguard-home no usa la imagen nueva")
	}
	f.mu.Lock()
	creates := 0
	for _, e := range f.events {
		if strings.HasPrefix(e, "create ") {
			creates++
		}
	}
	policy := f.ctrs["adguard-home"].host["RestartPolicy"]
	f.mu.Unlock()
	if creates != 1 {
		t.Errorf("solo debía recrearse adguard-home: %d", creates)
	}
	if m, _ := policy.(map[string]any); m["Name"] != "always" {
		t.Errorf("el contenedor nuevo debe conservar su política de reinicio: %v", policy)
	}
	f.noLeftovers(t)
}

// Si el principal no arranca con la versión nueva, todo el grupo vuelve a la
// anterior.
func TestUpdatePrincipalFailsRevertsGroup(t *testing.T) {
	f := homeServer()
	f.broken["sha256:mdb2"] = true
	a := testApp(t, f)
	a.updates.Items = []Update{{Container: "nextcloud-db", Image: "mariadb:11", State: "available"}}
	j, err := a.ApplyUpdate("prueba", "nextcloud-db", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if end := waitJob(t, a, j.ID); end.State != "error" || !strings.Contains(end.Message, "Todo el grupo ha vuelto") {
		t.Fatalf("debía fallar y volver atrás: %s %s", end.State, end.Message)
	}
	f.mu.Lock()
	for _, n := range []string{"nextcloud-db", "nextcloud-respaldo"} {
		c := f.ctrs[n]
		if c == nil || c.image != "sha256:mdb1" || !c.running {
			t.Errorf("%s debía volver a la versión anterior y en marcha: %+v", n, c)
		}
		if m, _ := c.host["RestartPolicy"].(map[string]any); m["Name"] != "always" {
			t.Errorf("%s debía recuperar su política de reinicio: %v", n, c.host["RestartPolicy"])
		}
	}
	f.mu.Unlock()
	f.noLeftovers(t)
	if u, _ := a.findUpdate("nextcloud-db"); u.State != "available" || u.Error == nil {
		t.Errorf("la actualización debe seguir pendiente con el error: %+v", u)
	}
}

// Un hermano que se quedó con una versión anterior se alinea sin descargar.
func TestAlignBehindSibling(t *testing.T) {
	f := homeServer()
	f.images["postgres:17-alpine"] = "sha256:pg17b" // alguien actualizó solo notas-db
	f.cfgs["sha256:pg17b"] = f.cfgs["sha256:pg17"]
	f.ctrs["notas-db"].image = "sha256:pg17b"
	a := testApp(t, f)
	a.updates.Items = []Update{{Container: "notas-db", Image: "postgres:17-alpine", State: "behind", Siblings: []Sibling{{Name: "notas-respaldo-1", Role: "aux", Behind: true}}}}
	dbID := f.idOf("notas-db")
	j, err := a.AlignUpdate("prueba", "notas-db", nil)
	if err != nil {
		t.Fatal(err)
	}
	if end := waitJob(t, a, j.ID); end.State != "done" {
		t.Fatalf("terminó en %s: %s", end.State, end.Message)
	}
	if f.has("pull postgres:17-alpine") {
		t.Error("alinear no descarga nada")
	}
	if f.imageOf("notas-respaldo-1") != "sha256:pg17b" || f.idOf("notas-db") != dbID {
		t.Error("solo debía recrearse el respaldo, con la imagen local")
	}
	if _, ok := a.findUpdate("notas-db"); ok {
		t.Error("sin nada pendiente, la entrada desaparece")
	}
}

// Si Nextcloud no se recupera, se queda en mantenimiento con el cron
// detenido; «Arrancar auxiliares» solo funciona cuando ya se resolvió.
func TestNextcloudStuckThenStartAux(t *testing.T) {
	f := homeServer()
	f.crashy["sha256:nc2"] = true
	a := testApp(t, f)
	a.updates.Items = []Update{{Container: "nextcloud-app", Image: "nextcloud:latest", State: "available", Siblings: []Sibling{{Name: "nextcloud-cron", Role: "aux"}}}}
	j, err := a.ApplyUpdate("prueba", "nextcloud-app", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if end := waitJob(t, a, j.ID); end.State != "error" || !strings.Contains(end.Message, "modo de mantenimiento") {
		t.Fatalf("debía quedarse en mantenimiento: %s %s", end.State, end.Message)
	}
	u, _ := a.findUpdate("nextcloud-app")
	f.mu.Lock()
	cronRunning, maint, kept := f.ctrs["nextcloud-cron"].running, f.maint, f.ctrs["nextcloud-app-anterior"] != nil
	f.mu.Unlock()
	if cronRunning || !maint || !slices.Equal(u.Waiting, []string{"nextcloud-cron"}) || u.Siblings[0].State != "waiting" {
		t.Fatalf("el cron debe quedar detenido a la espera: %+v", u)
	}
	if !kept {
		t.Error("el contenedor anterior se conserva mientras el grupo no termine bien")
	}
	if _, err := a.ApplyUpdate("prueba", "nextcloud-app", false, nil); err == nil {
		t.Error("no se puede volver a actualizar con auxiliares a la espera")
	}
	if _, err := a.StartWaiting("prueba", "nextcloud-app"); err == nil || !strings.Contains(err.Error(), "mantenimiento") {
		t.Errorf("con Nextcloud en mantenimiento no se arrancan: %v", err)
	}
	// Se resuelve a mano.
	f.mu.Lock()
	f.maint, f.crashy["sha256:nc2"] = false, false
	f.mu.Unlock()
	reload(t, a)
	j, err = a.StartWaiting("prueba", "nextcloud-app")
	if err != nil {
		t.Fatal(err)
	}
	if end := waitJob(t, a, j.ID); end.State != "done" {
		t.Fatalf("terminó en %s: %s", end.State, end.Message)
	}
	if f.imageOf("nextcloud-cron") != "sha256:nc2" || !f.has("start nextcloud-cron") {
		t.Error("el cron debe arrancar con la versión nueva")
	}
	if u, _ := a.findUpdate("nextcloud-app"); len(u.Waiting) != 0 {
		t.Errorf("ya no queda nadie a la espera: %v", u.Waiting)
	}
	f.noLeftovers(t)
}

// Un desmarcado no se toca; los que usan la red de otro se reconectan; y
// revertir devuelve el grupo a la versión anterior.
func TestSkipLinkedAndRevert(t *testing.T) {
	f := newFake()
	f.images["web:1"], f.cfgs["sha256:viejo"] = "sha256:viejo", fakeImg{[]string{"web"}, nil}
	f.images["vpn:2"], f.cfgs["sha256:vpn"] = "sha256:vpn", fakeImg{[]string{"vpn"}, nil}
	f.pulls["web:1"] = "sha256:nuevo"
	web := f.add("web", "web:1", "app", nil, "web_data")
	f.add("worker", "web:1", "app", []string{"worker"}, "web_data")
	f.add("otra", "web:1", "", nil)
	vpn := f.add("vpn", "vpn:2", "", nil)
	vpn.host["NetworkMode"] = "container:" + web.id
	a := testApp(t, f)
	a.updates.Items = []Update{{Container: "web", Image: "web:1", State: "available"}}
	j, err := a.ApplyUpdate("prueba", "web", false, []string{"otra"})
	if err != nil {
		t.Fatal(err)
	}
	if end := waitJob(t, a, j.ID); end.State != "done" {
		t.Fatalf("terminó en %s: %s", end.State, end.Message)
	}
	if f.imageOf("web") != "sha256:nuevo" || f.imageOf("worker") != "sha256:nuevo" || f.imageOf("otra") != "sha256:viejo" {
		t.Errorf("web y worker con la nueva; otra, desmarcada, con la anterior: %s %s %s", f.imageOf("web"), f.imageOf("worker"), f.imageOf("otra"))
	}
	f.mu.Lock()
	if v := f.ctrs["vpn"]; v.host["NetworkMode"] != "container:"+f.ctrs["web"].id || !v.running || v.image != "sha256:vpn" {
		t.Errorf("vpn no se reconectó al contenedor nuevo: %+v", v)
	}
	f.mu.Unlock()
	f.noLeftovers(t)

	reload(t, a)
	j, err = a.RevertUpdate("prueba", "web")
	if err != nil {
		t.Fatal(err)
	}
	if end := waitJob(t, a, j.ID); end.State != "done" {
		t.Fatalf("la reversión terminó en %s: %s", end.State, end.Message)
	}
	if f.imageOf("web") != "sha256:viejo" || f.imageOf("worker") != "sha256:viejo" {
		t.Error("el grupo debía volver a la versión anterior")
	}
	f.mu.Lock()
	if v := f.ctrs["vpn"]; v.host["NetworkMode"] != "container:"+f.ctrs["web"].id || !v.running {
		t.Errorf("vpn no se reconectó tras revertir: %+v", v)
	}
	f.mu.Unlock()
	f.noLeftovers(t)
}
