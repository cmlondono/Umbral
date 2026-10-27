package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// CheckUpdates compara el digest local de cada imagen en uso con el del
// registro. Solo detecta versiones nuevas de la misma etiqueta.
func (a *App) CheckUpdates() {
	a.mu.Lock()
	if a.updates.Checking || !a.docker.Available {
		a.mu.Unlock()
		return
	}
	a.updates.Checking = true
	cs := append([]Container{}, a.docker.Containers...)
	prev := map[string]Update{}
	for _, u := range a.updates.Items {
		prev[u.Container] = u
	}
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	now := time.Now()
	var items []Update
	digests := map[string]string{}
	for _, c := range cs {
		if strings.HasPrefix(c.Image, "sha256:") || strings.Contains(c.Image, "@") || strings.HasSuffix(c.Name, "-anterior") {
			continue
		}
		p := prev[c.Name]
		// Una actualización hecha hace menos de 7 días se conserva para poder revertirla.
		if p.State == "updated" && p.PreviousImageID != nil && now.Sub(p.CheckedAt) < 7*24*time.Hour {
			items = append(items, p)
			continue
		}
		ref := c.Image
		if !strings.Contains(ref, ":") {
			ref += ":latest"
		}
		remote, ok := digests[ref]
		if !ok {
			r, err := RemoteDigest(ctx, ref)
			if err != nil {
				continue
			}
			remote, digests[ref] = r, r
		}
		_, local, err := a.Docker.ImageID(ctx, ref)
		if err != nil || len(local) == 0 || digestMatches(local, remote) {
			continue
		}
		items = append(items, Update{Container: c.Name, Image: ref, State: "available", CheckedAt: now})
	}
	if items == nil {
		items = []Update{}
	}
	a.mu.Lock()
	a.updates = UpdatesState{LastCheck: &now, Items: items}
	a.mu.Unlock()
	a.saveUpdates()
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

func (a *App) findUpdate(name string) (Update, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, u := range a.updates.Items {
		if u.Container == name {
			return u, true
		}
	}
	return Update{}, false
}

// ApplyUpdate descarga la versión nueva y recrea el contenedor con la misma
// configuración y los mismos volúmenes. Funciona igual con contenedores de
// Docker Compose: no necesita leer su archivo. La imagen anterior se conserva
// para poder revertir.
func (a *App) ApplyUpdate(user, name string) (*Job, error) {
	u, ok := a.findUpdate(name)
	if !ok || u.State != "available" {
		return nil, errors.New("No hay ninguna actualización pendiente para " + name + ".")
	}
	if a.busy("", name) {
		return nil, errors.New("Ya hay una acción en curso para " + name + ".")
	}
	nc := isNextcloudImage(u.Image)
	steps := []string{"Descargando la versión nueva de " + u.Image, "Comprobando que la versión es compatible"}
	if nc {
		steps = append(steps, "Activando el modo de mantenimiento", "Copia de seguridad de la base de datos")
	}
	steps = append(steps, "Recreando el contenedor "+name)
	if nc {
		steps = append(steps, "Esperando a que Nextcloud termine de actualizarse", "Desactivando el modo de mantenimiento")
	} else {
		steps = append(steps, "Comprobando que arranca")
	}
	j := a.newJob("update", name, steps...)
	a.setUpdate(name, func(x *Update) { x.State, x.Error = "updating", nil })
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()
		step := 0
		next := func() { step++; j.step(step) }
		fail := func(err error) {
			e := err.Error()
			a.setUpdate(name, func(x *Update) { x.State, x.Error = "available", &e })
			a.Log(user, name, "Actualización", err, "")
			j.fail(e)
			a.Refresh("docker")
			a.Refresh("slow")
		}
		j.step(0)
		raw, err := a.Docker.Inspect(ctx, name)
		if err != nil {
			fail(err)
			return
		}
		var oldImg string
		_ = json.Unmarshal(raw["Image"], &oldImg)
		if err := a.Docker.Pull(ctx, u.Image, func(p int, _ string) { j.set(func(j *Job) { j.Progress = p / 3 }) }); err != nil {
			fail(err)
			return
		}
		next()
		if err := a.checkMajorJump(ctx, oldImg, u.Image); err != nil {
			fail(err)
			return
		}
		if !nc {
			next()
			if err := a.swapImage(ctx, name, raw, u.Image); err != nil {
				fail(err)
				return
			}
			next()
			if err := a.waitRunning(ctx, name); err != nil {
				fail(err)
				return
			}
			a.setUpdate(name, func(x *Update) { x.State, x.PreviousImageID, x.CheckedAt = "updated", &oldImg, time.Now() })
			a.Log(user, name, "Actualización", nil, u.Image)
			a.Refresh("docker")
			j.finish(name+" ya usa la versión nueva de "+u.Image+".", nil)
			return
		}

		// Nextcloud: nada se toca hasta tener la base de datos copiada.
		db, kind := a.findDatabase(raw)
		if db == "" {
			fail(errors.New("No encontré el contenedor de la base de datos de Nextcloud en marcha, así que no puedo hacer la copia de seguridad. No se ha tocado nada."))
			return
		}
		next()
		if _, err := a.occ(ctx, name, "maintenance:mode", "--on"); err != nil {
			fail(fmt.Errorf("No se pudo activar el modo de mantenimiento, así que no se ha tocado nada: %v", err))
			return
		}
		maintOff := func() { _, _ = a.occ(context.Background(), name, "maintenance:mode", "--off") }
		next()
		backup, err := a.backupDatabase(ctx, db, kind)
		if err != nil {
			maintOff()
			fail(fmt.Errorf("La copia de seguridad de %s falló, así que no se ha actualizado nada y Nextcloud vuelve a funcionar: %v", db, err))
			return
		}
		a.Log(user, db, "Copia de seguridad", nil, backup)
		next()
		if err := a.swapImage(ctx, name, raw, u.Image); err != nil {
			maintOff()
			fail(fmt.Errorf("%v. Nextcloud sigue con la versión anterior y vuelve a funcionar.", err))
			return
		}
		next()
		want := a.Docker.ImageEnv(ctx, u.Image, "NEXTCLOUD_VERSION")
		if err := a.waitNextcloudUpgrade(ctx, name, want, func(m string) { j.set(func(j *Job) { j.Message = m }) }); err != nil {
			e := fmt.Errorf("La actualización no terminó bien (%v). Para proteger los datos, Nextcloud se queda en modo de mantenimiento. La copia de la base de datos está en %s. Revisa qué pasó con: docker logs %s", err, backup, name)
			a.setUpdate(name, func(x *Update) { s := e.Error(); x.State, x.Error, x.PreviousImageID = "updated", &s, &oldImg })
			a.Log(user, name, "Actualización", e, "")
			j.fail(e.Error())
			a.Refresh("docker")
			a.Refresh("slow")
			return
		}
		next()
		if _, err := a.occ(ctx, name, "maintenance:mode", "--off"); err != nil {
			e := fmt.Errorf("Nextcloud ya está actualizado, pero no se pudo desactivar el modo de mantenimiento: %v. Desactívalo con: docker exec -u www-data %s php occ maintenance:mode --off", err, name)
			a.setUpdate(name, func(x *Update) { s := e.Error(); x.State, x.Error, x.PreviousImageID = "updated", &s, &oldImg })
			j.fail(e.Error())
			a.Refresh("slow")
			return
		}
		st, _ := a.ncStatus(ctx, name)
		a.setUpdate(name, func(x *Update) { x.State, x.PreviousImageID, x.CheckedAt = "updated", &oldImg, time.Now() })
		a.Log(user, name, "Actualización", nil, "Nextcloud "+st.VersionString+". Copia previa: "+backup)
		a.Refresh("docker")
		a.Refresh("slow")
		j.finish("Nextcloud ya está en la versión "+st.VersionString+" y vuelve a funcionar. La copia de la base de datos anterior está en "+backup+".", map[string]string{"backup": backup})
	}()
	return j, nil
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

// swapImage cambia el contenedor por uno igual con otra imagen. Si algo
// falla, deja el anterior como estaba.
func (a *App) swapImage(ctx context.Context, name string, raw map[string]json.RawMessage, image string) error {
	old := name + "-anterior"
	_ = a.Docker.ContainerAction(ctx, old, "remove")
	if err := a.Docker.ContainerAction(ctx, name, "stop"); err != nil {
		return err
	}
	if err := a.Docker.Rename(ctx, name, old); err != nil {
		_ = a.Docker.ContainerAction(ctx, name, "start")
		return err
	}
	id, err := a.Docker.Recreate(ctx, raw, name, image)
	if err == nil {
		err = a.Docker.ContainerAction(ctx, id, "start")
	}
	if err != nil {
		if id != "" {
			_ = a.Docker.ContainerAction(ctx, id, "stop")
			_ = a.Docker.ContainerAction(ctx, id, "remove")
		}
		_ = a.Docker.Rename(ctx, old, name)
		_ = a.Docker.ContainerAction(ctx, name, "start")
		return fmt.Errorf("no se pudo crear el contenedor nuevo; se mantiene el anterior: %v", err)
	}
	// El contenedor anterior ya no hace falta: para revertir basta su imagen.
	_ = a.Docker.ContainerAction(ctx, old, "remove")
	return nil
}

func (a *App) waitRunning(ctx context.Context, name string) error {
	for i := 0; i < 10; i++ {
		time.Sleep(2 * time.Second)
		r, err := a.Docker.Inspect(ctx, name)
		if err != nil {
			continue
		}
		var st struct {
			Running    bool
			Restarting bool
		}
		_ = json.Unmarshal(r["State"], &st)
		if st.Restarting || !st.Running {
			if i >= 4 {
				return errors.New("La versión nueva no arranca. Revierte para volver a la anterior y revisa los registros con docker logs " + name + ".")
			}
			continue
		}
		if i >= 2 {
			return nil
		}
	}
	return nil
}

func (a *App) RevertUpdate(user, name string) (*Job, error) {
	u, ok := a.findUpdate(name)
	if !ok || u.State != "updated" || u.PreviousImageID == nil {
		return nil, errors.New("No hay una versión anterior guardada para " + name + ".")
	}
	if a.busy("", name) {
		return nil, errors.New("Ya hay una acción en curso para " + name + ".")
	}
	if strings.HasPrefix(u.Image, "nextcloud") {
		return nil, errors.New("Nextcloud no se puede revertir desde el panel: al arrancar la versión nueva ya actualizó su base de datos, y la versión anterior no funcionaría con ella. Para volver atrás hay que restaurar una copia de seguridad.")
	}
	j := a.newJob("revert", name, "Volviendo a la versión anterior", "Comprobando que arranca")
	a.setUpdate(name, func(x *Update) { x.State = "reverting" })
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		fail := func(err error) {
			e := err.Error()
			a.setUpdate(name, func(x *Update) { x.State, x.Error = "updated", &e })
			a.Log(user, name, "Reversión", err, "")
			j.fail(e)
			a.Refresh("docker")
		}
		j.step(0)
		if _, _, err := a.Docker.ImageID(ctx, *u.PreviousImageID); err != nil {
			fail(errors.New("La imagen anterior ya no está en el servidor (quizá se liberó espacio en Imágenes), así que no se puede revertir."))
			return
		}
		raw, err := a.Docker.Inspect(ctx, name)
		if err != nil {
			fail(err)
			return
		}
		if err := a.swapImage(ctx, name, raw, *u.PreviousImageID); err != nil {
			fail(err)
			return
		}
		j.step(1)
		if err := a.waitRunning(ctx, name); err != nil {
			fail(err)
			return
		}
		a.setUpdate(name, func(x *Update) { x.State, x.PreviousImageID, x.Error = "available", nil, nil })
		a.Log(user, name, "Reversión", nil, "Vuelve a la versión anterior de "+u.Image)
		a.Refresh("docker")
		j.finish(name+" vuelve a usar la versión anterior.", nil)
	}()
	return j, nil
}
