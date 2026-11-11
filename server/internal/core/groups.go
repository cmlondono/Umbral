package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Grupos de contenedores con la misma imagen (repo:tag). Al actualizarla se
// recrean todos, en este orden: los auxiliares del principal se detienen, se
// recrea el principal, después los auxiliares y, al final, los independientes.
// Nada depende de nombres: el papel de cada uno sale de sus volúmenes, de su
// proyecto de Docker Compose y de si usa el entrypoint de la imagen.

// Sibling: otro contenedor con la misma imagen que el principal.
type Sibling struct {
	Name string `json:"name"`
	// Role: «aux» si comparte volumen con el principal o es de su mismo
	// proyecto con otro entrypoint (cron, worker, respaldo); «independent» si no.
	Role string `json:"role"`
	// Project: el proyecto de Docker Compose; vacío si no usa Compose.
	Project      string `json:"project,omitempty"`
	OtherProject bool   `json:"otherProject,omitempty"`
	// Behind: usa una imagen distinta de la que tiene ahora su etiqueta.
	Behind bool `json:"behind,omitempty"`
	// State: «waiting» (detenido a la espera de que Nextcloud se recupere) o
	// «failed» (no se pudo recrear y sigue con la versión anterior).
	State string `json:"state,omitempty"`
	Note  string `json:"note,omitempty"`
}

type ctrInfo struct {
	c       Container
	raw     map[string]json.RawMessage
	image   string // ID de la imagen con la que corre
	project string
	mounts  map[string]bool
	// own: usa el entrypoint y el command de su imagen.
	own     bool
	running bool
}

func (x ctrInfo) shares(o ctrInfo) bool {
	for k := range x.mounts {
		if o.mounts[k] {
			return true
		}
	}
	return false
}

func mountKeys(raw map[string]json.RawMessage) map[string]bool {
	var ms []struct{ Type, Name, Source string }
	_ = json.Unmarshal(raw["Mounts"], &ms)
	out := map[string]bool{}
	for _, m := range ms {
		switch {
		case m.Type == "volume" && m.Name != "":
			out["v:"+m.Name] = true
		case m.Type == "bind" && m.Source != "":
			out["b:"+m.Source] = true
		}
	}
	return out
}

func (a *App) inspectAll(ctx context.Context, cs []Container) ([]ctrInfo, error) {
	type cmd struct{ ep, cmd []string }
	imgs := map[string]cmd{}
	var out []ctrInfo
	for _, c := range cs {
		raw, err := a.Docker.Inspect(ctx, c.ID)
		if err != nil {
			return nil, fmt.Errorf("no se pudo leer la configuración de %s: %v", c.Name, err)
		}
		var cfg struct {
			Entrypoint, Cmd []string
			Labels          map[string]string
		}
		_ = json.Unmarshal(raw["Config"], &cfg)
		x := ctrInfo{c: c, raw: raw, project: cfg.Labels["com.docker.compose.project"], mounts: mountKeys(raw), running: isRunning(raw)}
		_ = json.Unmarshal(raw["Image"], &x.image)
		ic, ok := imgs[x.image]
		if !ok {
			ic.ep, ic.cmd, _ = a.Docker.ImageConfig(ctx, x.image)
			imgs[x.image] = ic
		}
		x.own = slices.Equal(cfg.Entrypoint, ic.ep) && slices.Equal(cfg.Cmd, ic.cmd)
		out = append(out, x)
	}
	return out, nil
}

// roleOf: auxiliar si comparte un volumen con el principal o es de su mismo
// proyecto de Compose con otro entrypoint o command. Uno de otro proyecto sin
// volúmenes en común es independiente aunque su entrypoint sea distinto.
func roleOf(p, x ctrInfo) string {
	if x.shares(p) || (p.project != "" && x.project == p.project && !x.own) {
		return "aux"
	}
	return "independent"
}

// pickPrincipal: si se pulsó en un auxiliar, el principal es el hermano de su
// mismo proyecto (o con un volumen en común) que usa el entrypoint de la
// imagen. Sin clic, el que usa el entrypoint de la imagen, mejor con página web.
func pickPrincipal(infos []ctrInfo, clicked string) int {
	at := slices.IndexFunc(infos, func(x ctrInfo) bool { return x.c.Name == clicked })
	if at >= 0 {
		x := infos[at]
		if x.own {
			return at
		}
		best := -1
		for i, y := range infos {
			if i == at || !y.own || !((x.project != "" && y.project == x.project) || x.shares(y)) {
				continue
			}
			if best < 0 || (y.c.URL != nil && infos[best].c.URL == nil) {
				best = i
			}
		}
		if best >= 0 {
			return best
		}
		return at
	}
	score := func(y ctrInfo) int {
		s := 0
		if y.own {
			s += 2
		}
		if y.c.URL != nil {
			s++
		}
		return s
	}
	best := 0
	for i := range infos {
		if score(infos[i]) > score(infos[best]) {
			best = i
		}
	}
	return best
}

// unitsOf reparte un grupo en unidades; la primera posición de cada una es su
// principal. Con Nextcloud, cada instancia (la que usa el entrypoint de la
// imagen) es una unidad con sus auxiliares, porque necesita su propio modo de
// mantenimiento y su propia copia. Con las demás imágenes, el grupo entero es
// una unidad.
func unitsOf(infos []ctrInfo, special bool) [][]ctrInfo {
	var heads []int
	if special {
		for i, x := range infos {
			if x.own {
				heads = append(heads, i)
			}
		}
	}
	if len(heads) < 2 {
		return [][]ctrInfo{withFirst(infos, pickPrincipal(infos, ""))}
	}
	slices.SortStableFunc(heads, func(i, j int) int {
		if (infos[i].c.URL != nil) == (infos[j].c.URL != nil) {
			return 0
		}
		if infos[i].c.URL != nil {
			return -1
		}
		return 1
	})
	units := make([][]ctrInfo, len(heads))
	for k, h := range heads {
		units[k] = []ctrInfo{infos[h]}
	}
	for i, x := range infos {
		if slices.Contains(heads, i) {
			continue
		}
		k := 0
		for j, h := range heads {
			if roleOf(infos[h], x) == "aux" {
				k = j
				break
			}
		}
		units[k] = append(units[k], x)
	}
	return units
}

func withFirst(infos []ctrInfo, i int) []ctrInfo {
	out := []ctrInfo{infos[i]}
	for j, x := range infos {
		if j != i {
			out = append(out, x)
		}
	}
	return out
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

/* ---------- Actualizar, alinear y arrancar auxiliares ---------- */

// groupRun: una actualización (o alineación) de un grupo en curso.
type groupRun struct {
	a               *App
	j               *Job
	user, key, img  string
	mode, prevState string
	p               ctrInfo
	doP             bool
	aux, indep      []ctrInfo
	linked          []linkedCtr
	nc, pull        bool
	keys            []string
	ids             map[string]string // ID anterior → ID nuevo
	done            []string          // contenedores recreados, para borrar su «-anterior»
	prevAlso        map[string]string
	notes           map[string]string // avisos por contenedor
	warnings        []string
	backup          string
}

func (g *groupRun) at(key string) {
	if i := slices.Index(g.keys, key); i >= 0 {
		g.j.step(i)
	}
}

func (g *groupRun) action() string {
	if g.mode == "align" {
		return "Alineación"
	}
	return "Actualización"
}

// ApplyUpdate descarga la versión nueva una sola vez y recrea el grupo de
// contenedores que usan la imagen, con la misma configuración, volúmenes y
// etiquetas de Compose. skip: hermanos que el usuario desmarcó.
func (a *App) ApplyUpdate(user, name string, toMajor bool, skip []string) (*Job, error) {
	mode := "apply"
	if toMajor {
		mode = "major"
	}
	return a.startGroup(user, name, mode, skip)
}

// AlignUpdate recrea, sin descargar nada, los contenedores del grupo que usan
// una versión anterior de la imagen que ya tiene su etiqueta.
func (a *App) AlignUpdate(user, name string, skip []string) (*Job, error) {
	return a.startGroup(user, name, "align", skip)
}

func (a *App) startGroup(user, name, mode string, skip []string) (*Job, error) {
	u, ok := a.findUpdate(name)
	img := u.Image
	switch {
	case !ok:
		return nil, errors.New("No hay ninguna actualización pendiente para " + name + ".")
	case mode == "major" && u.NextMajor == "":
		return nil, errors.New("No hay una versión mayor nueva para " + name + ".")
	case mode == "major" && u.State == "available":
		return nil, errors.New("Primero aplica la actualización pendiente de tu versión actual: Nextcloud pide estar al día antes de cambiar de versión mayor.")
	case mode == "major":
		img = u.NextMajor
	case mode == "align" && u.State != "behind":
		return nil, errors.New("Todos los contenedores de " + name + " ya usan la misma versión de la imagen.")
	case mode == "apply" && u.State != "available":
		return nil, errors.New("No hay ninguna actualización pendiente para " + name + ".")
	}
	if len(u.Waiting) > 0 {
		return nil, errors.New("Primero arranca los auxiliares que siguen detenidos tras la última actualización.")
	}
	ref := normRef(u.Image)
	a.mu.RLock()
	var members []Container
	for _, c := range a.docker.Containers {
		if updatable(c) && normRef(c.Image) == ref {
			members = append(members, c)
		}
	}
	a.mu.RUnlock()
	ctx0, cancel0 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel0()
	infos, err := a.inspectAll(ctx0, members)
	if err != nil {
		return nil, errors.New(upperFirst(err.Error()) + ".")
	}
	special := isNextcloudImage(ref)
	var unit []ctrInfo
	for _, un := range unitsOf(infos, special) {
		if slices.ContainsFunc(un, func(x ctrInfo) bool { return x.c.Name == name || x.c.Name == u.Container }) {
			unit = un
			break
		}
	}
	if unit == nil {
		return nil, errors.New("El contenedor " + name + " ya no existe.")
	}
	g := &groupRun{a: a, user: user, key: u.Container, img: img, mode: mode, prevState: u.State, pull: mode != "align",
		ids: map[string]string{}, prevAlso: map[string]string{}, notes: map[string]string{}}
	g.p = unit[pickPrincipal(unit, name)]
	g.nc = special && g.p.own
	localID := ""
	if mode == "align" {
		localID, _, _ = a.Docker.ImageID(ctx0, img)
	}
	behind := func(x ctrInfo) bool { return mode != "align" || x.image != localID }
	g.doP = behind(g.p)
	for _, x := range unit {
		if x.c.Name == g.p.c.Name || slices.Contains(skip, x.c.Name) || !behind(x) {
			continue
		}
		if roleOf(g.p, x) == "aux" {
			g.aux = append(g.aux, x)
		} else {
			g.indep = append(g.indep, x)
		}
	}
	all := append([]ctrInfo{g.p}, append(append([]ctrInfo{}, g.aux...), g.indep...)...)
	var cs []Container
	for _, x := range all {
		if a.busy("", x.c.Name) {
			return nil, errors.New("Ya hay una acción en curso para " + x.c.Name + ".")
		}
		cs = append(cs, x.c)
	}
	if !g.doP && len(g.aux)+len(g.indep) == 0 {
		return nil, errors.New("No queda ningún contenedor que recrear.")
	}
	g.linked = a.linkedTo(ctx0, cs)

	names := func(xs []ctrInfo) string {
		var n []string
		for _, x := range xs {
			n = append(n, x.c.Name)
		}
		return joinList(n)
	}
	var labels []string
	add := func(k, l string) { g.keys, labels = append(g.keys, k), append(labels, l) }
	if g.pull {
		add("pull", "Descargando la versión nueva de "+img)
	} else {
		add("pull", "Comprobando la versión local de "+img)
	}
	add("compat", "Comprobando que la versión es compatible")
	if g.nc && g.doP {
		add("maint", "Activando el modo de mantenimiento")
		add("backup", "Copia de seguridad de la base de datos")
	}
	if len(g.aux) > 0 && g.doP {
		add("stopaux", "Deteniendo los auxiliares: "+names(g.aux))
	}
	if g.doP {
		add("p", "Recreando el contenedor "+g.p.c.Name)
		if g.nc {
			add("waitp", "Esperando a que Nextcloud termine de actualizarse")
		} else {
			add("waitp", "Comprobando que "+g.p.c.Name+" arranca")
		}
	}
	if len(g.aux) > 0 {
		add("aux", "Recreando los auxiliares con la versión nueva: "+names(g.aux))
	}
	if g.nc && g.doP {
		add("maintoff", "Desactivando el modo de mantenimiento")
	}
	if len(g.indep) > 0 {
		add("indep", "Recreando los independientes: "+names(g.indep))
	}
	if len(g.linked) > 0 {
		var ln []string
		for _, l := range g.linked {
			ln = append(ln, l.name)
		}
		add("linked", "Reconectando los contenedores que comparten su red: "+joinList(ln))
	}
	kind := "update"
	if mode == "align" {
		kind = "align"
	}
	g.j = a.newJob(kind, g.key, labels...)
	a.setUpdate(g.key, func(x *Update) { x.State, x.Error = "updating", nil })
	go g.run()
	return g.j, nil
}

// fail deja todo el grupo como estaba y lo anota.
func (g *groupRun) fail(err error) {
	a, e := g.a, err.Error()
	a.setUpdate(g.key, func(x *Update) { x.State, x.Error = g.prevState, &e })
	a.Log(g.user, g.p.c.Name, g.action(), err, "")
	g.j.fail(e)
	a.Refresh("docker")
	a.Refresh("slow")
}

// recreateOne recrea un contenedor con otra imagen y comprueba que arranca.
// Si falla, lo intenta una vez más; si sigue fallando, ese contenedor vuelve
// a su versión anterior.
func (a *App) recreateOne(ctx context.Context, x ctrInfo, image string, start bool) (string, error) {
	var err error
	for try := 0; try < 2; try++ {
		var id string
		id, err = a.swap(ctx, x.c.Name, x.raw, image, start)
		if err == nil && start {
			if err = a.waitRunning(ctx, x.c.Name); err != nil {
				if back := a.restoreOld(ctx, x.c.Name, start, x.raw); back != nil {
					return "", fmt.Errorf("%v; tampoco se pudo volver al anterior: %v", err, back)
				}
			}
		}
		if err == nil {
			return id, nil
		}
	}
	return "", err
}

func (g *groupRun) recreate(ctx context.Context, x ctrInfo, start bool) {
	id, err := g.a.recreateOne(ctx, x, g.img, start)
	if err != nil {
		msg := x.c.Name + " no se pudo recrear (" + strings.TrimSuffix(err.Error(), ".") + ") y sigue con la versión anterior."
		g.notes[x.c.Name], g.warnings = msg, append(g.warnings, msg)
		g.a.Log(g.user, x.c.Name, g.action(), fmt.Errorf("%s → %s: %v", shortID(x.image), shortID(g.newImage(ctx)), err), "")
		return
	}
	g.ids[containerID(x.raw)] = id
	g.done = append(g.done, x.c.Name)
	g.prevAlso[x.c.Name] = x.image
	g.a.Log(g.user, x.c.Name, g.action(), nil, g.img+": "+shortID(x.image)+" → "+shortID(g.newImage(ctx)))
}

func (g *groupRun) newImage(ctx context.Context) string {
	id, _, _ := g.a.Docker.ImageID(ctx, g.img)
	return id
}

func (g *groupRun) run() {
	a := g.a
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	g.at("pull")
	if g.pull {
		if err := a.Docker.Pull(ctx, g.img, func(p int, _ string) { g.j.set(func(j *Job) { j.Progress = p / 3 }) }); err != nil {
			g.fail(err)
			return
		}
	} else if _, _, err := a.Docker.ImageID(ctx, g.img); err != nil {
		g.fail(errors.New("La imagen " + g.img + " ya no está en el servidor."))
		return
	}
	g.at("compat")
	if err := a.checkMajorJump(ctx, g.p.image, g.img); err != nil {
		g.fail(err)
		return
	}
	// Tras la descarga: los que ya usan la versión nueva no se tocan.
	newID := g.newImage(ctx)
	g.doP = g.doP && g.p.image != newID
	keep := func(xs []ctrInfo) []ctrInfo {
		return slices.DeleteFunc(xs, func(x ctrInfo) bool { return x.image == newID })
	}
	g.aux, g.indep = keep(g.aux), keep(g.indep)

	maintOff := func() {}
	if g.nc && g.doP {
		// Nextcloud: nada se toca hasta tener la base de datos copiada.
		db, kind := a.findDatabase(g.p.raw)
		if db == "" {
			g.fail(errors.New("No encontré el contenedor de la base de datos de Nextcloud en marcha, así que no puedo hacer la copia de seguridad. No se ha tocado nada."))
			return
		}
		g.at("maint")
		if _, err := a.occ(ctx, g.p.c.Name, "maintenance:mode", "--on"); err != nil {
			g.fail(fmt.Errorf("No se pudo activar el modo de mantenimiento, así que no se ha tocado nada: %v", err))
			return
		}
		maintOff = func() { _, _ = a.occ(context.Background(), g.p.c.Name, "maintenance:mode", "--off") }
		g.at("backup")
		backup, err := a.backupDatabase(ctx, db, kind)
		if err != nil {
			maintOff()
			g.fail(fmt.Errorf("La copia de seguridad de %s falló, así que no se ha actualizado nada y Nextcloud vuelve a funcionar: %v", db, err))
			return
		}
		g.backup = backup
		a.Log(g.user, db, "Copia de seguridad", nil, backup)
	}

	if g.doP {
		// Los auxiliares (cron, workers, respaldos) se detienen para que no
		// haya una ejecución a medias mientras cambia el principal.
		g.at("stopaux")
		for _, x := range g.aux {
			if x.running {
				_ = a.Docker.ContainerAction(ctx, x.c.Name, "stop")
			}
		}
		a.stopLinked(ctx, g.linked)
		undo := func() {
			for _, x := range g.aux {
				if x.running {
					_ = a.Docker.ContainerAction(context.Background(), x.c.Name, "start")
				}
			}
			a.startLinked(context.Background(), g.linked)
			maintOff()
		}
		g.at("p")
		id, err := a.swap(ctx, g.p.c.Name, g.p.raw, g.img, true)
		if err != nil {
			undo()
			msg := fmt.Sprintf("%v. Todo el grupo sigue con la versión anterior.", strings.TrimSuffix(err.Error(), "."))
			if g.nc {
				msg += " Nextcloud vuelve a funcionar."
			}
			g.fail(errors.New(upperFirst(msg)))
			return
		}
		g.ids[containerID(g.p.raw)] = id
		g.at("waitp")
		if g.nc {
			want := a.Docker.ImageEnv(ctx, g.img, "NEXTCLOUD_VERSION")
			if err := a.waitNextcloudUpgrade(ctx, g.p.c.Name, want, func(m string) { g.j.set(func(j *Job) { j.Message = m }) }); err != nil {
				g.ncStuck(ctx, err)
				return
			}
		} else if err := a.waitRunning(ctx, g.p.c.Name); err != nil {
			back := a.restoreOld(ctx, g.p.c.Name, true, g.p.raw)
			undo()
			if back != nil {
				g.fail(fmt.Errorf("%s %v, y no se pudo volver a la versión anterior: %v", g.p.c.Name, err, back))
			} else {
				g.fail(fmt.Errorf("La versión nueva de %s %v. Todo el grupo ha vuelto a la versión anterior.", g.p.c.Name, err))
			}
			return
		}
		g.done = append(g.done, g.p.c.Name)
		a.Log(g.user, g.p.c.Name, g.action(), nil, g.img+": "+shortID(g.p.image)+" → "+shortID(newID))
	}
	if len(g.aux) > 0 {
		g.at("aux")
		for _, x := range g.aux {
			g.recreate(ctx, x, x.running)
		}
	}
	if g.nc && g.doP {
		g.at("maintoff")
		if _, err := a.occ(ctx, g.p.c.Name, "maintenance:mode", "--off"); err != nil {
			g.warnings = append(g.warnings, fmt.Sprintf("Nextcloud ya está actualizado, pero no se pudo desactivar el modo de mantenimiento: %v. Desactívalo con: docker exec -u www-data %s php occ maintenance:mode --off", err, g.p.c.Name))
		}
	}
	if len(g.indep) > 0 {
		g.at("indep")
		for _, x := range g.indep {
			g.recreate(ctx, x, x.running)
		}
	}
	if len(g.linked) > 0 {
		g.at("linked")
		d, f := a.relink(ctx, g.linked, g.ids)
		g.done, g.warnings = append(g.done, d...), append(g.warnings, f...)
	}
	g.end(ctx)
}

// end anota el resultado. Los «-anterior» se borran solo si todo el grupo
// marcado terminó bien; mientras tanto quedan detenidos y sin reinicio
// automático.
func (g *groupRun) end(ctx context.Context) {
	a := g.a
	if len(g.warnings) == 0 {
		a.removeOld(ctx, g.done)
	}
	var e *string
	if len(g.warnings) > 0 {
		s := strings.Join(g.warnings, " ")
		e = &s
	}
	if g.mode == "align" && !g.doP && e == nil {
		a.dropUpdate(g.key)
	} else {
		a.setUpdate(g.key, func(x *Update) {
			if g.doP {
				old := g.p.image
				x.State, x.PreviousImageID, x.Image, x.NextMajor = "updated", &old, g.img, ""
			} else {
				x.State = g.prevState
			}
			x.CheckedAt, x.Error = time.Now(), e
			x.PreviousAlso = maps0(g.prevAlso)
			for i := range x.Siblings {
				s := &x.Siblings[i]
				if _, ok := g.prevAlso[s.Name]; ok {
					s.Behind, s.State, s.Note = false, "", ""
				} else if n := g.notes[s.Name]; n != "" {
					s.State, s.Note = "failed", n
				}
			}
		})
	}
	a.Refresh("docker")
	a.Refresh("slow")
	msg := g.p.c.Name + " ya usa la versión nueva de " + g.img + "."
	switch {
	case g.mode == "align":
		msg = "Todo el grupo de " + g.p.c.Name + " usa ya la misma versión de " + g.img + "."
	case g.nc:
		st, _ := a.ncStatus(ctx, g.p.c.Name)
		msg = "Nextcloud ya está en la versión " + st.VersionString + " y vuelve a funcionar. La copia de la base de datos anterior está en " + g.backup + "."
	}
	if len(g.done) > 1 {
		msg += " También se recrearon: " + joinList(slices.DeleteFunc(append([]string{}, g.done...), func(n string) bool { return n == g.p.c.Name })) + "."
	}
	if e != nil {
		g.j.fail(msg + " Pero: " + *e)
		return
	}
	var extra map[string]string
	if g.backup != "" {
		extra = map[string]string{"backup": g.backup}
	}
	g.j.finish(msg, extra)
}

// ncStuck: Nextcloud no terminó de actualizarse. No se revierte, porque su
// base de datos ya puede estar migrada: se queda en mantenimiento y los
// auxiliares, detenidos hasta que se resuelva.
func (g *groupRun) ncStuck(ctx context.Context, err error) {
	a := g.a
	var waiting []string
	for _, x := range g.aux {
		if x.running {
			waiting = append(waiting, x.c.Name)
		}
	}
	msg := fmt.Sprintf("La actualización no terminó bien (%v). Para proteger los datos, Nextcloud se queda en modo de mantenimiento. La copia de la base de datos está en %s. Revisa qué pasó con: docker logs %s", err, g.backup, g.p.c.Name)
	if len(waiting) > 0 {
		msg += ". Mientras tanto, " + joinList(waiting) + " sigue detenido a la espera de que Nextcloud se recupere."
	}
	old := g.p.image
	a.setUpdate(g.key, func(x *Update) {
		x.State, x.Error, x.PreviousImageID, x.Image, x.CheckedAt = "updated", &msg, &old, g.img, time.Now()
		x.Waiting = waiting
		for i := range x.Siblings {
			if slices.Contains(waiting, x.Siblings[i].Name) {
				x.Siblings[i].State, x.Siblings[i].Note = "waiting", "Detenido a la espera de que Nextcloud se recupere."
			}
		}
	})
	a.Log(g.user, g.p.c.Name, g.action(), errors.New(msg), "")
	g.j.fail(msg)
	a.Refresh("docker")
	a.Refresh("slow")
}

// StartWaiting arranca, con la versión nueva, los auxiliares que una
// actualización fallida de Nextcloud dejó detenidos. Solo cuando Nextcloud ya
// funciona: fuera del modo de mantenimiento y sin actualización pendiente.
func (a *App) StartWaiting(user, name string) (*Job, error) {
	u, ok := a.findUpdate(name)
	if !ok || len(u.Waiting) == 0 {
		return nil, errors.New("No hay auxiliares detenidos para " + name + ".")
	}
	for _, n := range append([]string{u.Container}, u.Waiting...) {
		if a.busy("", n) {
			return nil, errors.New("Ya hay una acción en curso para " + n + ".")
		}
	}
	ctx0, cancel0 := context.WithTimeout(context.Background(), time.Minute)
	defer cancel0()
	if isNextcloudImage(u.Image) {
		st, err := a.ncStatus(ctx0, u.Container)
		switch {
		case err != nil:
			return nil, errors.New("Nextcloud todavía no responde, así que los auxiliares siguen detenidos: " + err.Error())
		case st.Maintenance:
			return nil, errors.New("Nextcloud sigue en modo de mantenimiento. Resuélvelo antes de arrancar los auxiliares.")
		case st.NeedsDBUpgrade:
			return nil, errors.New("Nextcloud aún tiene la base de datos sin actualizar. Resuélvelo antes de arrancar los auxiliares.")
		}
	}
	a.mu.RLock()
	var cs []Container
	for _, c := range a.docker.Containers {
		if slices.Contains(u.Waiting, c.Name) {
			cs = append(cs, c)
		}
	}
	a.mu.RUnlock()
	infos, err := a.inspectAll(ctx0, cs)
	if err != nil {
		return nil, errors.New(upperFirst(err.Error()) + ".")
	}
	j := a.newJob("aux", u.Container, "Arrancando "+joinList(u.Waiting)+" con la versión nueva")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		j.step(0)
		newID, _, _ := a.Docker.ImageID(ctx, u.Image)
		var failed, started []string
		for _, x := range infos {
			var err error
			if x.image == newID {
				if err = a.Docker.ContainerAction(ctx, x.c.Name, "start"); err == nil {
					err = a.waitRunning(ctx, x.c.Name)
				}
			} else {
				_, err = a.recreateOne(ctx, x, u.Image, true)
			}
			a.Log(user, x.c.Name, "Arrancar auxiliar", err, u.Image+": "+shortID(x.image)+" → "+shortID(newID))
			if err != nil {
				failed = append(failed, x.c.Name+" no arranca ("+strings.TrimSuffix(err.Error(), ".")+").")
				continue
			}
			started = append(started, x.c.Name)
		}
		if len(failed) == 0 {
			a.removeOld(ctx, append([]string{u.Container}, started...))
		}
		a.setUpdate(u.Container, func(x *Update) {
			x.Waiting = slices.DeleteFunc(x.Waiting, func(n string) bool { return slices.Contains(started, n) })
			for i := range x.Siblings {
				if slices.Contains(started, x.Siblings[i].Name) {
					x.Siblings[i].State, x.Siblings[i].Note, x.Siblings[i].Behind = "", "", false
				}
			}
		})
		a.Refresh("docker")
		if len(failed) > 0 {
			j.fail(strings.Join(failed, " "))
			return
		}
		j.finish(joinList(started)+" vuelve a funcionar con la versión nueva.", nil)
	}()
	return j, nil
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

func maps0(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}
