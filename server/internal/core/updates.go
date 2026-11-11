package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Update struct {
	Container       string    `json:"container"`
	Image           string    `json:"image"`
	State           string    `json:"state"`
	CheckedAt       time.Time `json:"checkedAt"`
	PreviousImageID *string   `json:"previousImageId"`
	Error           *string   `json:"error"`
	// NextMajor: la etiqueta de la siguiente versión mayor (solo Nextcloud), p. ej. nextcloud:31-apache.
	NextMajor string `json:"nextMajor,omitempty"`
	// Behind: el principal usa una imagen distinta de la que tiene su etiqueta.
	Behind bool `json:"behind,omitempty"`
	// Siblings: los demás contenedores con la misma imagen y su papel.
	Siblings []Sibling `json:"siblings,omitempty"`
	// SameImage: contenedores con el mismo ID de imagen pero otra etiqueta
	// («nombre (etiqueta)»). Solo informativo: no se recrean.
	SameImage []string `json:"sameImage,omitempty"`
	// Linked: contenedores que usan la red, los procesos o la IPC de alguno
	// del grupo (network_mode: container:…). Se recrean al final para que se
	// enganchen al contenedor nuevo.
	Linked []string `json:"linked,omitempty"`
	// PreviousAlso: la imagen anterior de cada hermano recreado, para revertir.
	PreviousAlso map[string]string `json:"previousAlso,omitempty"`
	// Waiting: auxiliares detenidos a la espera de que Nextcloud se recupere.
	Waiting []string `json:"waiting,omitempty"`
}

type UpdatesState struct {
	LastCheck *time.Time `json:"lastCheck"`
	Checking  bool       `json:"checking"`
	Items     []Update   `json:"items"`
}

func (a *App) saveUpdates() {
	a.mu.RLock()
	u := a.updates
	u.Items = append([]Update{}, a.updates.Items...)
	a.mu.RUnlock()
	_ = a.updFile.Save(u)
}

// normRef: nextcloud → nextcloud:latest, para comparar referencias.
func normRef(ref string) string {
	if strings.Contains(ref, "@") {
		return ref
	}
	repo, tag := splitRef(ref)
	return repo + ":" + tag
}

// updatable: los contenedores fijados a un ID o a un digest no se comparan
// con el registro, y los «-anterior» son restos de una actualización.
func updatable(c Container) bool {
	return !strings.HasPrefix(c.Image, "sha256:") && !strings.Contains(c.Image, "@") && !strings.HasSuffix(c.Name, "-anterior")
}

func joinList(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " y " + names[len(names)-1]
}

// CheckUpdates compara el digest local de cada imagen en uso con el del
// registro. Solo detecta versiones nuevas de la misma etiqueta. Los
// contenedores con la misma etiqueta forman un grupo que se actualiza junto.
// También detecta los que usan una versión anterior de la imagen que ya tiene
// su etiqueta (se descargó para otro contenedor o con docker compose pull).
func (a *App) CheckUpdates() {
	a.mu.Lock()
	if a.updates.Checking || !a.docker.Available {
		a.mu.Unlock()
		return
	}
	a.updates.Checking = true
	cs := append([]Container{}, a.docker.Containers...)
	prev := append([]Update{}, a.updates.Items...)
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	now := time.Now()
	var items []Update
	exists, covered := map[string]bool{}, map[string]bool{}
	for _, c := range cs {
		exists[c.Name] = true
	}
	// Una actualización hecha hace menos de 7 días se conserva para poder
	// revertirla, igual que una con auxiliares a la espera.
	for _, p := range prev {
		keep := p.State == "updated" && p.PreviousImageID != nil && now.Sub(p.CheckedAt) < 7*24*time.Hour
		if !(keep || len(p.Waiting) > 0) || !exists[p.Container] {
			continue
		}
		// Solo siguen en él los hermanos recreados o a la espera; los demás
		// se vuelven a comparar.
		p.Siblings = slices.DeleteFunc(slices.Clone(p.Siblings), func(s Sibling) bool {
			_, done := p.PreviousAlso[s.Name]
			return !done && !slices.Contains(p.Waiting, s.Name)
		})
		items = append(items, p)
		covered[p.Container] = true
		for n := range p.PreviousAlso {
			covered[n] = true
		}
		for _, n := range p.Waiting {
			covered[n] = true
		}
	}
	var refs []string
	groups := map[string][]Container{}
	for _, c := range cs {
		if !updatable(c) || covered[c.Name] {
			continue
		}
		ref := normRef(c.Image)
		if groups[ref] == nil {
			refs = append(refs, ref)
		}
		groups[ref] = append(groups[ref], c)
	}
	for _, ref := range refs {
		localID, local, err := a.Docker.ImageID(ctx, ref)
		if err != nil || len(local) == 0 {
			continue // sin digest del registro: imagen compilada en el servidor
		}
		infos, err := a.inspectAll(ctx, groups[ref])
		if err != nil {
			continue
		}
		remote, err := RemoteDigest(ctx, ref)
		newer := err == nil && !digestMatches(local, remote)
		for _, unit := range unitsOf(infos, isNextcloudImage(ref)) {
			state := ""
			switch {
			case newer:
				state = "available"
			case slices.ContainsFunc(unit, func(x ctrInfo) bool { return x.image != localID }):
				state = "behind"
			default:
				continue
			}
			items = append(items, a.unitUpdate(ctx, unit, ref, state, localID, now, cs))
		}
	}
	// Nextcloud con una etiqueta de versión fija (30, 30-apache, 30.0.2-fpm…):
	// ¿existe ya la siguiente mayor? Basta una petición HEAD al registro.
	var mrefs []string
	majors := map[string][]Container{}
	for _, c := range cs {
		if !isNextcloudImage(c.Image) || !updatable(c) {
			continue
		}
		ref := normRef(c.Image)
		if majors[ref] == nil {
			mrefs = append(mrefs, ref)
		}
		majors[ref] = append(majors[ref], c)
	}
	for _, ref := range mrefs {
		next, ok := nextMajorRef(ref)
		if !ok {
			continue
		}
		if _, err := RemoteDigest(ctx, next); err != nil {
			continue
		}
		found := false
		for i := range items {
			if normRef(items[i].Image) == ref {
				items[i].NextMajor, found = next, true
			}
		}
		if found {
			continue
		}
		localID, _, _ := a.Docker.ImageID(ctx, ref)
		infos, err := a.inspectAll(ctx, majors[ref])
		if err != nil {
			continue
		}
		for _, unit := range unitsOf(infos, true) {
			u := a.unitUpdate(ctx, unit, ref, "major", localID, now, cs)
			u.NextMajor = next
			items = append(items, u)
		}
	}
	if items == nil {
		items = []Update{}
	}
	a.mu.Lock()
	a.updates = UpdatesState{LastCheck: &now, Items: items}
	a.mu.Unlock()
	a.saveUpdates()
}

// unitUpdate describe una unidad: el principal (unit[0]), sus hermanos con su
// papel y los contenedores relacionados.
func (a *App) unitUpdate(ctx context.Context, unit []ctrInfo, ref, state, localID string, now time.Time, cs []Container) Update {
	p := unit[0]
	u := Update{Container: p.c.Name, Image: ref, State: state, CheckedAt: now, Behind: p.image != localID}
	ids := map[string]bool{}
	var group []Container
	for i, x := range unit {
		ids[x.image] = true
		group = append(group, x.c)
		if i > 0 {
			u.Siblings = append(u.Siblings, Sibling{Name: x.c.Name, Role: roleOf(p, x), Project: x.project, OtherProject: x.project != p.project, Behind: x.image != localID})
		}
	}
	for _, c := range cs {
		if !strings.HasSuffix(c.Name, "-anterior") && normRef(c.Image) != ref && ids[c.ImageID] {
			u.SameImage = append(u.SameImage, c.Name+" ("+c.Image+")")
		}
	}
	for _, l := range a.linkedTo(ctx, group) {
		u.Linked = append(u.Linked, l.name)
	}
	return u
}

func (a *App) setUpdate(name string, f func(u *Update)) {
	a.mu.Lock()
	for i := range a.updates.Items {
		if a.updates.Items[i].Container == name {
			f(&a.updates.Items[i])
		}
	}
	a.mu.Unlock()
	a.saveUpdates()
}

// dropUpdate quita una entrada que ya no tiene nada pendiente.
func (a *App) dropUpdate(name string) {
	a.mu.Lock()
	a.updates.Items = slices.DeleteFunc(slices.Clone(a.updates.Items), func(u Update) bool { return u.Container == name })
	a.mu.Unlock()
	a.saveUpdates()
}

// clearSolvedUpdateErrors quita el aviso de una actualización que terminó con
// error cuando el contenedor ya funciona bien (por ejemplo, tras arreglarlo a
// mano). El fallo sigue en el historial de acciones.
func (a *App) clearSolvedUpdateErrors(cs []Container) {
	byName := map[string]Container{}
	for _, c := range cs {
		byName[c.Name] = c
	}
	a.mu.RLock()
	nc := a.nextcloud
	var solved []string
	for _, u := range a.updates.Items {
		c, ok := byName[u.Container]
		if u.Error == nil || u.State == "updating" || !ok || c.State != "running" || c.Health == "unhealthy" || c.Health == "starting" {
			continue
		}
		if isNextcloudImage(u.Image) && (!nc.Reachable || (nc.Maintenance != nil && *nc.Maintenance)) {
			continue
		}
		if slices.ContainsFunc(u.Siblings, func(s Sibling) bool { return s.State == "failed" }) {
			continue // el aviso de un hermano que no se pudo recrear se queda hasta la próxima comprobación
		}
		solved = append(solved, u.Container)
	}
	a.mu.RUnlock()
	for _, name := range solved {
		if a.busy("", name) {
			continue
		}
		a.setUpdate(name, func(x *Update) { x.Error = nil })
	}
}

// findUpdate busca la entrada de un contenedor, sea el principal o un hermano.
func (a *App) findUpdate(name string) (Update, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, u := range a.updates.Items {
		if u.Container == name {
			return u, true
		}
	}
	for _, u := range a.updates.Items {
		if slices.ContainsFunc(u.Siblings, func(s Sibling) bool { return s.Name == name }) {
			return u, true
		}
	}
	return Update{}, false
}

func isRunning(raw map[string]json.RawMessage) bool {
	var st struct{ Running bool }
	_ = json.Unmarshal(raw["State"], &st)
	return st.Running
}

func containerID(raw map[string]json.RawMessage) string {
	var id string
	_ = json.Unmarshal(raw["Id"], &id)
	return id
}

// swap cambia el contenedor por uno igual con otra imagen y deja el anterior
// detenido, sin reinicio automático, como <nombre>-anterior hasta que se llame
// a removeOld. Si algo falla, deja el anterior como estaba.
func (a *App) swap(ctx context.Context, name string, raw map[string]json.RawMessage, image string, start bool) (string, error) {
	old := name + "-anterior"
	_ = a.Docker.ContainerAction(ctx, old, "remove")
	if err := a.Docker.ContainerAction(ctx, name, "stop"); err != nil && !notModified(err) {
		return "", err
	}
	if err := a.Docker.Rename(ctx, name, old); err != nil {
		if start {
			_ = a.Docker.ContainerAction(ctx, name, "start")
		}
		return "", err
	}
	// Con restart: always, Docker lo arrancaría al reiniciar el servidor.
	_ = a.Docker.SetRestartPolicy(ctx, old, map[string]any{"Name": "no"})
	id, err := a.Docker.Recreate(ctx, raw, name, image)
	if err == nil && start {
		err = a.Docker.ContainerAction(ctx, id, "start")
	}
	if err != nil {
		if id != "" {
			_ = a.Docker.ContainerAction(ctx, id, "stop")
			_ = a.Docker.ContainerAction(ctx, id, "remove")
		}
		_ = a.Docker.Rename(ctx, old, name)
		a.restorePolicy(ctx, name, raw)
		if start {
			_ = a.Docker.ContainerAction(ctx, name, "start")
		}
		return "", fmt.Errorf("no se pudo crear el contenedor nuevo; se mantiene el anterior: %v", err)
	}
	return id, nil
}

func (a *App) restorePolicy(ctx context.Context, name string, raw map[string]json.RawMessage) {
	var host struct{ RestartPolicy map[string]any }
	if json.Unmarshal(raw["HostConfig"], &host) == nil && host.RestartPolicy != nil {
		_ = a.Docker.SetRestartPolicy(ctx, name, host.RestartPolicy)
	}
}

// swapImage recrea un solo contenedor con otra imagen y reconecta los que
// comparten su red.
func (a *App) swapImage(ctx context.Context, name string, raw map[string]json.RawMessage, image string) error {
	a.mu.RLock()
	var self []Container
	for _, c := range a.docker.Containers {
		if c.Name == name {
			self = append(self, c)
		}
	}
	a.mu.RUnlock()
	linked := a.linkedTo(ctx, self)
	a.stopLinked(ctx, linked)
	id, err := a.swap(ctx, name, raw, image, true)
	if err != nil {
		a.startLinked(ctx, linked)
		return err
	}
	done, failed := a.relink(ctx, linked, map[string]string{containerID(raw): id})
	if len(failed) > 0 {
		return errors.New(strings.Join(failed, " "))
	}
	a.removeOld(ctx, append(done, name))
	return nil
}

// restoreOld vuelve al contenedor anterior que swap dejó como <nombre>-anterior.
func (a *App) restoreOld(ctx context.Context, name string, start bool, raw map[string]json.RawMessage) error {
	_ = a.Docker.ContainerAction(ctx, name, "stop")
	if err := a.Docker.ContainerAction(ctx, name, "remove"); err != nil {
		return err
	}
	if err := a.Docker.Rename(ctx, name+"-anterior", name); err != nil {
		return err
	}
	a.restorePolicy(ctx, name, raw)
	if start {
		return a.Docker.ContainerAction(ctx, name, "start")
	}
	return nil
}

// removeOld borra los contenedores anteriores. Para revertir basta su imagen,
// que no se borra: Docker no deja borrar una imagen que use un contenedor.
func (a *App) removeOld(ctx context.Context, names []string) {
	for _, n := range names {
		_ = a.Docker.ContainerAction(ctx, n+"-anterior", "remove")
	}
}

/* ---------- Contenedores que comparten la red de otro ---------- */

// linkedCtr: un contenedor con network_mode, pid o ipc «container:…».
type linkedCtr struct {
	name    string
	raw     map[string]json.RawMessage
	running bool
}

// linkedTo busca los contenedores que usan la red, los procesos o la IPC de
// alguno del grupo. Al recrear el grupo quedan enganchados al contenedor
// anterior, que desaparece, y se quedarían sin red.
func (a *App) linkedTo(ctx context.Context, group []Container) []linkedCtr {
	in := map[string]bool{}
	for _, c := range group {
		in[c.Name] = true
	}
	a.mu.RLock()
	cs := append([]Container{}, a.docker.Containers...)
	a.mu.RUnlock()
	var out []linkedCtr
	for _, c := range cs {
		if in[c.Name] || strings.HasSuffix(c.Name, "-anterior") {
			continue
		}
		raw, err := a.Docker.Inspect(ctx, c.ID)
		if err != nil || !sharesWith(raw, group) {
			continue
		}
		out = append(out, linkedCtr{name: c.Name, raw: raw, running: isRunning(raw)})
	}
	return out
}

var sharedModes = []string{"NetworkMode", "PidMode", "IpcMode"}

func sharesWith(raw map[string]json.RawMessage, group []Container) bool {
	var host map[string]any
	_ = json.Unmarshal(raw["HostConfig"], &host)
	for _, k := range sharedModes {
		v, _ := host[k].(string)
		t, ok := strings.CutPrefix(v, "container:")
		if !ok || t == "" {
			continue
		}
		for _, c := range group {
			if t == c.Name || (len(t) >= 12 && strings.HasPrefix(c.ID, t)) {
				return true
			}
		}
	}
	return false
}

// retarget cambia «container:<ID anterior>» por el ID nuevo. Las referencias
// por nombre no cambian: el contenedor nuevo se llama igual.
func retarget(raw map[string]json.RawMessage, ids map[string]string) map[string]json.RawMessage {
	var host map[string]any
	if json.Unmarshal(raw["HostConfig"], &host) != nil || host == nil {
		return raw
	}
	for _, k := range sharedModes {
		v, _ := host[k].(string)
		t, ok := strings.CutPrefix(v, "container:")
		if !ok || len(t) < 12 {
			continue
		}
		for old, nw := range ids {
			if strings.HasPrefix(old, t) {
				host[k] = "container:" + nw
			}
		}
	}
	b, err := json.Marshal(host)
	if err != nil {
		return raw
	}
	out := maps.Clone(raw)
	out["HostConfig"] = b
	return out
}

func (a *App) stopLinked(ctx context.Context, linked []linkedCtr) {
	for _, l := range linked {
		if l.running {
			_ = a.Docker.ContainerAction(ctx, l.name, "stop")
		}
	}
}

func (a *App) startLinked(ctx context.Context, linked []linkedCtr) {
	for _, l := range linked {
		if l.running {
			_ = a.Docker.ContainerAction(ctx, l.name, "start")
		}
	}
}

// relink recrea los contenedores enganchados al grupo con su misma imagen,
// apuntando al contenedor nuevo. Solo arranca los que estaban en marcha.
func (a *App) relink(ctx context.Context, linked []linkedCtr, ids map[string]string) (done, failed []string) {
	for _, l := range linked {
		var cfg struct{ Image string }
		var imgID string
		_ = json.Unmarshal(l.raw["Config"], &cfg)
		_ = json.Unmarshal(l.raw["Image"], &imgID)
		// Con su etiqueta si sigue apuntando a la misma imagen; si no, con el
		// ID, para no actualizarlo sin pedirlo.
		image := imgID
		if id, _, err := a.Docker.ImageID(ctx, cfg.Image); cfg.Image != "" && err == nil && id == imgID {
			image = cfg.Image
		}
		_, err := a.swap(ctx, l.name, retarget(l.raw, ids), image, l.running)
		if err == nil && l.running {
			err = a.waitRunning(ctx, l.name)
		}
		if err != nil {
			failed = append(failed, "No se pudo reconectar "+l.name+" ("+strings.TrimSuffix(err.Error(), ".")+"): recréalo a mano con docker compose up -d.")
			continue
		}
		done = append(done, l.name)
	}
	return done, failed
}

// checkMajorJump evita las actualizaciones que la aplicación no admite. Hoy
// solo lo sabe de Nextcloud: no permite saltarse versiones mayores ni volver
// atrás una vez actualizada su base de datos.
func (a *App) checkMajorJump(ctx context.Context, oldRef, newRef string) error {
	oldV := a.Docker.ImageEnv(ctx, oldRef, "NEXTCLOUD_VERSION")
	newV := a.Docker.ImageEnv(ctx, newRef, "NEXTCLOUD_VERSION")
	if oldV == "" || newV == "" {
		return nil
	}
	a.mu.RLock()
	if a.nextcloud.Version != nil && *a.nextcloud.Version != "" {
		oldV = *a.nextcloud.Version
	}
	a.mu.RUnlock()
	om, nm := major(oldV), major(newV)
	switch {
	case nm > om+1:
		return fmt.Errorf("Nextcloud no permite saltarse versiones mayores: tienes la %s y la imagen nueva trae la %s. Actualiza primero a la %d cambiando la etiqueta de la imagen a nextcloud:%d en tu docker-compose.", oldV, newV, om+1, om+1)
	case nm < om:
		return fmt.Errorf("La imagen trae Nextcloud %s, más antigua que la %s que tienes. Nextcloud no permite volver atrás.", newV, oldV)
	}
	return nil
}

func major(v string) int {
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// pollEvery: cada cuánto se mira si un contenedor recreado sigue en marcha.
var pollEvery = 2 * time.Second

// waitRunning espera a que un contenedor recreado esté sano: con
// healthcheck, hasta que diga «healthy» (unos 3 minutos como mucho); sin él,
// en marcha y sin reiniciarse durante unos segundos.
func (a *App) waitRunning(ctx context.Context, name string) error {
	for i := 0; i < 90; i++ {
		time.Sleep(pollEvery)
		r, err := a.Docker.Inspect(ctx, name)
		if err != nil {
			continue
		}
		var st struct {
			Running    bool
			Restarting bool
			Health     *struct{ Status string }
		}
		_ = json.Unmarshal(r["State"], &st)
		if st.Restarting || !st.Running {
			if i >= 4 {
				return errors.New("no arranca; revisa sus registros con docker logs " + name)
			}
			continue
		}
		if st.Health != nil {
			switch st.Health.Status {
			case "healthy":
				return nil
			case "unhealthy":
				return errors.New("arranca, pero su comprobación de salud falla; revisa sus registros con docker logs " + name)
			}
			continue
		}
		if i >= 2 {
			return nil
		}
	}
	return errors.New("su comprobación de salud no termina; revisa sus registros con docker logs " + name)
}

// RevertUpdate vuelve a la imagen anterior en todo el grupo: primero el
// principal, con sus auxiliares detenidos, y después cada hermano.
func (a *App) RevertUpdate(user, name string) (*Job, error) {
	u, ok := a.findUpdate(name)
	if !ok || u.State != "updated" || u.PreviousImageID == nil {
		return nil, errors.New("No hay una versión anterior guardada para " + name + ".")
	}
	if isNextcloudImage(u.Image) {
		return nil, errors.New("Nextcloud no se puede revertir desde el panel: al arrancar la versión nueva ya actualizó su base de datos, y la versión anterior no funcionaría con ella. Para volver atrás hay que restaurar una copia de seguridad.")
	}
	a.mu.RLock()
	var cs []Container
	for _, c := range a.docker.Containers {
		if _, also := u.PreviousAlso[c.Name]; c.Name == u.Container || also {
			cs = append(cs, c)
		}
	}
	a.mu.RUnlock()
	for _, c := range cs {
		if a.busy("", c.Name) {
			return nil, errors.New("Ya hay una acción en curso para " + c.Name + ".")
		}
	}
	ctx0, cancel0 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel0()
	infos, err := a.inspectAll(ctx0, cs)
	if err != nil {
		return nil, errors.New(upperFirst(err.Error()) + ".")
	}
	pi := slices.IndexFunc(infos, func(x ctrInfo) bool { return x.c.Name == u.Container })
	if pi < 0 {
		return nil, errors.New("El contenedor " + u.Container + " ya no existe.")
	}
	p := infos[pi]
	others := withFirst(infos, pi)[1:]
	var names []string
	for _, x := range others {
		names = append(names, x.c.Name)
	}
	linked := a.linkedTo(ctx0, cs)
	steps := []string{"Volviendo a la versión anterior de " + p.c.Name, "Comprobando que arranca"}
	if len(others) > 0 {
		steps = append(steps, "Volviendo a la versión anterior en "+joinList(names))
	}
	if len(linked) > 0 {
		var ln []string
		for _, l := range linked {
			ln = append(ln, l.name)
		}
		steps = append(steps, "Reconectando los contenedores que comparten su red: "+joinList(ln))
	}
	j := a.newJob("revert", u.Container, steps...)
	a.setUpdate(u.Container, func(x *Update) { x.State = "reverting" })
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		step := 0
		next := func() { step++; j.step(step) }
		fail := func(err error) {
			e := err.Error()
			a.setUpdate(u.Container, func(x *Update) { x.State, x.Error = "updated", &e })
			a.Log(user, p.c.Name, "Reversión", err, "")
			j.fail(e)
			a.Refresh("docker")
		}
		j.step(0)
		prev := *u.PreviousImageID
		if _, _, err := a.Docker.ImageID(ctx, prev); err != nil {
			fail(errors.New("La imagen anterior ya no está en el servidor (quizá se liberó espacio en Imágenes), así que no se puede revertir."))
			return
		}
		var aux []ctrInfo
		for _, x := range others {
			if roleOf(p, x) == "aux" && x.running {
				aux = append(aux, x)
				_ = a.Docker.ContainerAction(ctx, x.c.Name, "stop")
			}
		}
		a.stopLinked(ctx, linked)
		undo := func() {
			for _, x := range aux {
				_ = a.Docker.ContainerAction(context.Background(), x.c.Name, "start")
			}
			a.startLinked(context.Background(), linked)
		}
		id, err := a.swap(ctx, p.c.Name, p.raw, prev, true)
		if err != nil {
			undo()
			fail(err)
			return
		}
		next()
		if err := a.waitRunning(ctx, p.c.Name); err != nil {
			back := a.restoreOld(ctx, p.c.Name, true, p.raw)
			undo()
			if back != nil {
				fail(fmt.Errorf("La versión anterior de %s no arranca y no se pudo volver a la nueva: %v", p.c.Name, back))
			} else {
				fail(fmt.Errorf("La versión anterior de %s %v. Todo el grupo sigue con la nueva.", p.c.Name, err))
			}
			return
		}
		a.Log(user, p.c.Name, "Reversión", nil, shortID(p.image)+" → "+shortID(prev))
		ids := map[string]string{containerID(p.raw): id}
		done := []string{p.c.Name}
		var failed []string
		if len(others) > 0 {
			next()
			for _, x := range others {
				img := u.PreviousAlso[x.c.Name]
				if _, _, err := a.Docker.ImageID(ctx, img); err != nil {
					failed = append(failed, x.c.Name+" sigue con la versión nueva: su imagen anterior ya no está en el servidor.")
					continue
				}
				id, err := a.recreateOne(ctx, x, img, x.running)
				a.Log(user, x.c.Name, "Reversión", err, shortID(x.image)+" → "+shortID(img))
				if err != nil {
					failed = append(failed, x.c.Name+" no se pudo revertir ("+strings.TrimSuffix(err.Error(), ".")+") y sigue con la versión nueva.")
					continue
				}
				ids[containerID(x.raw)] = id
				done = append(done, x.c.Name)
			}
		}
		if len(linked) > 0 {
			next()
			d, f := a.relink(ctx, linked, ids)
			done, failed = append(done, d...), append(failed, f...)
		}
		if len(failed) == 0 {
			a.removeOld(ctx, done)
		}
		var e *string
		if len(failed) > 0 {
			s := strings.Join(failed, " ")
			e = &s
		}
		a.setUpdate(u.Container, func(x *Update) { x.State, x.PreviousImageID, x.PreviousAlso, x.Error = "available", nil, nil, e })
		a.Refresh("docker")
		if e != nil {
			j.fail(p.c.Name + " vuelve a usar la versión anterior. Pero: " + *e)
			return
		}
		j.finish(p.c.Name+" vuelve a usar la versión anterior.", nil)
	}()
	return j, nil
}

var reMajorTag = regexp.MustCompile(`^(\d+)(?:\.\d+)*(-[a-z][a-z0-9.-]*)?$`)

// nextMajorRef: nextcloud:30-apache → nextcloud:31-apache. Las etiquetas
// móviles (latest, stable, apache…) ya reciben las versiones mayores solas.
func nextMajorRef(ref string) (string, bool) {
	repo, tag := splitRef(ref)
	m := reMajorTag.FindStringSubmatch(tag)
	if m == nil {
		return "", false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 {
		return "", false
	}
	return fmt.Sprintf("%s:%d%s", repo, n+1, m[2]), true
}
