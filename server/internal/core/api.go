package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

const cookieName = "umbral_sesion"

type ctxKey struct{}

func userOf(r *http.Request) string { u, _ := r.Context().Value(ctxKey{}).(string); return u }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 256<<10))
	if err := dec.Decode(v); err != nil {
		return errors.New("La petición no es válida.")
	}
	return nil
}

// clientIP es la IP de quien hace la petición. Si llega desde el propio
// servidor con X-Forwarded-For, es «tailscale serve» haciendo de proxy: la IP
// real es la última de la cabecera (la que añade el proxy, no el cliente).
func clientIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if p := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); p != nil {
				return p
			}
		}
	}
	return ip
}

// Handler monta la API y sirve el panel compilado.
func (a *App) Handler(web fs.FS) http.Handler {
	mux := http.NewServeMux()
	open := func(p string, h http.HandlerFunc) { mux.HandleFunc(p, h) }
	auth := func(p string, h http.HandlerFunc) { mux.Handle(p, a.requireAuth(h)) }

	open("GET /api/auth/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]bool{"configured": a.Auth.Configured()})
	})
	open("POST /api/auth/login", a.login)
	open("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(cookieName); err == nil {
			if u, ok := a.Auth.Check(c.Value); ok {
				a.Access(r, u, "logout")
			}
			a.Auth.Logout(c.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	// Sin sesión responde 200 con el usuario vacío: no es un error.
	open("GET /api/auth/session", func(w http.ResponseWriter, r *http.Request) {
		user := ""
		if c, err := r.Cookie(cookieName); err == nil {
			user, _ = a.Auth.Check(c.Value)
		}
		writeJSON(w, 200, map[string]any{"user": user, "configured": a.Auth.Configured(), "totp": user != "" && a.Auth.TOTPEnabled(user), "recoveryLeft": a.Auth.RecoveryLeft(user)})
	})

	auth("POST /api/auth/password", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Current string `json:"current"`
			Next    string `json:"next"`
		}
		if err := readJSON(r, &body); err != nil {
			fail(w, 400, err.Error())
			return
		}
		user := userOf(r)
		if err := a.Auth.ChangePassword(user, body.Current, body.Next); err != nil {
			a.Log(user, "Inicio de sesión", "Cambio de contraseña", err, "")
			fail(w, 400, err.Error())
			return
		}
		a.Log(user, "Inicio de sesión", "Cambio de contraseña", nil, "Se cerraron las demás sesiones")
		a.Access(r, user, "password")
		// Cambiar la contraseña cierra todas las sesiones; se abre una nueva aquí.
		token, ttl := a.Auth.NewSession(user, true)
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int(ttl.Seconds()), Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"})
		writeJSON(w, 200, map[string]string{"user": user})
	})
	/* Doble factor: se activa y se quita desde la propia cuenta */
	auth("POST /api/auth/totp/begin", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Password string }
		_ = readJSON(r, &body)
		secret, uri, err := a.Auth.TOTPBegin(userOf(r), body.Password, "Umbral ("+a.Cfg.Get().ServerName+")")
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"secret": secret, "uri": uri})
	})
	auth("POST /api/auth/totp/confirm", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Code string }
		_ = readJSON(r, &body)
		codes, err := a.Auth.TOTPConfirm(userOf(r), body.Code)
		a.Log(userOf(r), "Inicio de sesión", "Doble factor activado", err, "")
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		a.Access(r, userOf(r), "totp-on")
		// Activarlo cierra las demás sesiones; esta sigue abierta.
		token, ttl := a.Auth.NewSession(userOf(r), true)
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int(ttl.Seconds()), Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"})
		writeJSON(w, 200, map[string]any{"recovery": codes})
	})
	auth("POST /api/auth/totp/disable", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Password, Code string }
		_ = readJSON(r, &body)
		err := a.Auth.TOTPDisable(userOf(r), body.Password, body.Code)
		a.Log(userOf(r), "Inicio de sesión", "Doble factor desactivado", err, "")
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		a.Access(r, userOf(r), "totp-off")
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	auth("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		a.Viewed()
		writeJSON(w, 200, a.Snapshot())
	})
	auth("POST /api/refresh", func(w http.ResponseWriter, r *http.Request) {
		a.Viewed()
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		a.sampleNow()
		if r.URL.Query().Get("red") == "1" {
			_ = a.Net.Scan(a.Cfg.Get())
		}
		a.refreshDocker(ctx, true)
		a.refreshSlow()
		a.refreshPC()
		writeJSON(w, 200, a.Snapshot())
	})
	auth("GET /api/metrics/history", func(w http.ResponseWriter, r *http.Request) {
		h, ok := a.Hist.Get(r.URL.Query().Get("range"))
		if !ok {
			fail(w, 400, "Periodo no válido.")
			return
		}
		writeJSON(w, 200, h)
	})
	auth("GET /api/metrics/live", func(w http.ResponseWriter, r *http.Request) {
		p, err := a.Live()
		if err != nil {
			fail(w, 503, err.Error())
			return
		}
		writeJSON(w, 200, p)
	})
	auth("GET /api/catalog", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, Catalog()) })
	auth("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		j, ok := a.Job(r.PathValue("id"))
		if !ok {
			fail(w, 404, "Esa tarea ya no existe.")
			return
		}
		writeJSON(w, 200, j)
	})

	/* Contenedores */
	auth("POST /api/containers/{name}/{action}", func(w http.ResponseWriter, r *http.Request) {
		name, action := r.PathValue("name"), r.PathValue("action")
		var body struct{ Confirm bool }
		_ = readJSON(r, &body)
		if action != "start" && !body.Confirm {
			fail(w, 400, "Esta acción necesita confirmación.")
			return
		}
		if action == "remove" && a.containerState(name) == "running" {
			fail(w, 409, "Detén el contenedor antes de eliminarlo.")
			return
		}
		verbs := map[string]string{"start": "Inicio", "stop": "Detención", "restart": "Reinicio", "remove": "Contenedor eliminado"}
		if verbs[action] == "" {
			fail(w, 404, "Acción desconocida.")
			return
		}
		err := a.Docker.ContainerAction(r.Context(), name, action)
		a.Log(userOf(r), name, verbs[action], err, "")
		a.refreshDocker(r.Context(), true)
		if err != nil {
			fail(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, a.Snapshot())
	})

	auth("POST /api/containers/{name}/backup", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		raw, err := a.Docker.Inspect(r.Context(), name)
		if err != nil {
			fail(w, 404, err.Error())
			return
		}
		db, kind := a.findDatabase(raw)
		if db == "" {
			fail(w, 404, "No encontré un contenedor de base de datos en marcha en la misma pila que "+name+".")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
		defer cancel()
		path, err := a.backupDatabase(ctx, db, kind)
		a.Log(userOf(r), db, "Copia de seguridad", err, path)
		if err != nil {
			fail(w, 502, "La copia falló: "+err.Error())
			return
		}
		st, _ := os.Stat(path)
		size := int64(0)
		if st != nil {
			size = st.Size()
		}
		writeJSON(w, 200, map[string]any{"path": path, "sizeKB": size / 1024, "database": db})
	})

	auth("GET /api/containers/{name}/logs", func(w http.ResponseWriter, r *http.Request) {
		tail := 200
		switch r.URL.Query().Get("lineas") {
		case "500":
			tail = 500
		case "2000":
			tail = 2000
		}
		lines, err := a.Docker.Logs(r.Context(), r.PathValue("name"), tail)
		if err != nil {
			fail(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"lines": lines, "at": time.Now()})
	})
	auth("GET /api/containers/{name}/details", func(w http.ResponseWriter, r *http.Request) {
		det, err := a.Docker.Details(r.Context(), r.PathValue("name"))
		if err != nil {
			fail(w, 404, err.Error())
			return
		}
		writeJSON(w, 200, det)
	})
	auth("POST /api/containers/{name}/memory", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MB      int  `json:"mb"`
			Confirm bool `json:"confirm"`
		}
		if readJSON(r, &body) != nil || !body.Confirm {
			fail(w, 400, "Esta acción necesita confirmación.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		if err := a.SetMemory(ctx, userOf(r), r.PathValue("name"), body.MB); err != nil {
			fail(w, 400, err.Error())
			return
		}
		a.refreshDocker(r.Context(), true)
		writeJSON(w, 200, a.Snapshot())
	})

	/* Router */
	type routerBody struct {
		RouterConfig
		Password *string `json:"password"`
	}
	auth("POST /api/router/test", func(w http.ResponseWriter, r *http.Request) {
		var b routerBody
		if err := readJSON(r, &b); err != nil {
			fail(w, 400, err.Error())
			return
		}
		if err := ValidRouter(b.RouterConfig); err != nil || b.Kind == "" {
			fail(w, 400, "Completa el tipo, la dirección y el usuario del router.")
			return
		}
		info, err := a.Router.Test(b.RouterConfig, b.Password)
		if err != nil {
			fail(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, info)
	})
	auth("PUT /api/router", func(w http.ResponseWriter, r *http.Request) {
		var b routerBody
		if err := readJSON(r, &b); err != nil {
			fail(w, 400, err.Error())
			return
		}
		p := a.Cfg.Public()
		p.Router = b.RouterConfig
		if b.Kind == "" {
			p.Router = RouterConfig{}
			empty := ""
			b.Password = &empty
		}
		if err := a.Cfg.Update(p); err != nil {
			fail(w, 400, err.Error())
			return
		}
		if err := a.Router.SetPassword(b.Password); err != nil {
			fail(w, 500, "No se pudo guardar la contraseña: "+err.Error())
			return
		}
		a.Log(userOf(r), "Router", map[bool]string{true: "Integración guardada", false: "Integración quitada"}[b.Kind != ""], nil, RouterKinds[b.Kind])
		a.refreshSlow()
		writeJSON(w, 200, a.Snapshot())
	})
	auth("POST /api/router/{op}", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			MAC     string `json:"mac"`
			Confirm bool   `json:"confirm"`
		}
		_ = readJSON(r, &b)
		op := r.PathValue("op")
		if (op == "block" || op == "reboot") && !b.Confirm {
			fail(w, 400, "Esta acción necesita confirmación.")
			return
		}
		err := a.Router.Do(a.Cfg.Get().Router, op, b.MAC)
		target := b.MAC
		if op == "reboot" {
			target = "Router"
		}
		a.Log(userOf(r), target, map[string]string{"block": "Internet bloqueado", "unblock": "Internet desbloqueado", "reboot": "Reinicio del router"}[op], err, "")
		if err != nil {
			fail(w, 502, err.Error())
			return
		}
		rs := a.Router.State()
		a.Net.SetRouter(a.Cfg.Get(), a.Router.Names(), rs.Blocked)
		writeJSON(w, 200, a.Snapshot())
	})

	/* Publicar en Tailscale con HTTPS */
	auth("POST /api/tailscale/serve", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Port  int    `json:"port"`
			Local int    `json:"local"`
			Label string `json:"label"`
		}
		if err := readJSON(r, &b); err != nil {
			fail(w, 400, err.Error())
			return
		}
		if err := a.Publish(userOf(r), b.Port, b.Local, b.Label); err != nil {
			fail(w, 502, err.Error())
			return
		}
		a.refreshSlow()
		writeJSON(w, 200, a.Snapshot())
	})
	auth("POST /api/tailscale/serve/{port}/off", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Confirm bool   `json:"confirm"`
			Label   string `json:"label"`
		}
		if readJSON(r, &b) != nil || !b.Confirm {
			fail(w, 400, "Esta acción necesita confirmación.")
			return
		}
		port, _ := strconv.Atoi(r.PathValue("port"))
		if err := a.Unpublish(userOf(r), port, b.Label); err != nil {
			fail(w, 502, err.Error())
			return
		}
		a.refreshSlow()
		writeJSON(w, 200, a.Snapshot())
	})

	/* Sistema: actualizaciones de Debian y salud de los discos */
	auth("POST /api/system/check", func(w http.ResponseWriter, r *http.Request) {
		err := a.Sys.Request()
		a.Sys.Refresh(true)
		if err != nil {
			fail(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, a.Snapshot())
	})

	/* Copias de seguridad */
	auth("GET /api/backups", func(w http.ResponseWriter, r *http.Request) {
		files, dir := a.Backups()
		writeJSON(w, 200, map[string]any{"files": files, "copyDir": dir, "ncFiles": a.ncFilesLog()})
	})
	auth("POST /api/backups/nextcloud", func(w http.ResponseWriter, r *http.Request) {
		j, err := a.BackupNextcloudFiles(userOf(r))
		if err != nil {
			fail(w, 409, err.Error())
			return
		}
		writeJSON(w, 202, j.copy())
	})
	auth("GET /api/backups/{name}", func(w http.ResponseWriter, r *http.Request) {
		p, err := a.BackupPath(r.PathValue("name"))
		if err != nil {
			fail(w, 404, err.Error())
			return
		}
		a.Log(userOf(r), "Copias de seguridad", "Descarga de una copia", nil, r.PathValue("name"))
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+r.PathValue("name")+"\"")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, p)
	})
	auth("POST /api/backups/copy", func(w http.ResponseWriter, r *http.Request) {
		n, err := a.CopyBackups(userOf(r))
		if err != nil {
			fail(w, 409, err.Error())
			return
		}
		files, dir := a.Backups()
		writeJSON(w, 200, map[string]any{"copied": n, "files": files, "copyDir": dir, "ncFiles": a.ncFilesLog()})
	})

	/* Avisos y accesos */
	auth("GET /api/access", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.AccessLog.List()) })
	auth("PUT /api/notify", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Server string  `json:"server"`
			Topic  string  `json:"topic"`
			Token  *string `json:"token"`
		}
		if err := readJSON(r, &body); err != nil {
			fail(w, 400, err.Error())
			return
		}
		if err := a.Notify.Update(body.Server, body.Topic, body.Token); err != nil {
			fail(w, 400, err.Error())
			return
		}
		a.Log(userOf(r), "Avisos", "Configuración guardada", nil, "")
		writeJSON(w, 200, a.Snapshot())
	})
	auth("GET /api/notify/topic", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"topic": NewTopic()})
	})
	auth("POST /api/notify/test", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		err := a.Notify.Send(ctx, "Umbral", "Aviso de prueba desde "+a.Cfg.Get().ServerName+". Si lo ves, los avisos funcionan.", 3, "white_check_mark")
		if err != nil {
			fail(w, 502, "No se pudo enviar: "+err.Error()+".")
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	/* Espacio de Docker */
	auth("GET /api/docker/disk", func(w http.ResponseWriter, r *http.Request) {
		d, err := a.Docker.Disk(r.Context())
		if err != nil {
			fail(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, d)
	})
	auth("POST /api/docker/build-cache/prune", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Confirm bool }
		if readJSON(r, &body) != nil || !body.Confirm {
			fail(w, 400, "Esta acción necesita confirmación.")
			return
		}
		mb, err := a.Docker.PruneBuildCache(r.Context())
		a.Log(userOf(r), "Docker", "Caché de compilación liberada", err, fmt.Sprintf("%d MB", mb))
		if err != nil {
			fail(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, map[string]int64{"freedMB": mb})
	})
	auth("POST /api/docker/volumes/{name}/remove", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Confirm bool }
		if readJSON(r, &body) != nil || !body.Confirm {
			fail(w, 400, "Esta acción necesita confirmación.")
			return
		}
		name := r.PathValue("name")
		err := a.Docker.RemoveVolume(r.Context(), name)
		a.Log(userOf(r), name, "Volumen eliminado", err, "")
		if err != nil {
			fail(w, 409, "No se pudo eliminar el volumen: "+err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	/* Imágenes */
	auth("POST /api/images/pull", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Ref string }
		if err := readJSON(r, &body); err != nil || !ValidImage(body.Ref) {
			fail(w, 400, "Formato no válido. Usa nombre:etiqueta, por ejemplo, nginx:1.27-alpine.")
			return
		}
		repo, tag := splitRef(body.Ref)
		ref := repo + ":" + tag
		user := userOf(r)
		j := a.newJob("pull", ref, "Descargando "+ref)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			j.step(0)
			err := a.Docker.Pull(ctx, ref, func(p int, msg string) { j.set(func(j *Job) { j.Progress, j.Message = p, msg }) })
			a.Log(user, ref, "Descarga de imagen", err, "")
			a.Refresh("docker")
			if err != nil {
				j.fail(err.Error())
				return
			}
			j.finish("Descargada. Ya aparece en la lista de imágenes.", map[string]string{"ref": ref})
		}()
		writeJSON(w, 202, j.copy())
	})
	auth("POST /api/images/remove", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Items   []struct{ ID, Ref string }
			Confirm bool
		}
		if err := readJSON(r, &body); err != nil || !body.Confirm || len(body.Items) == 0 {
			fail(w, 400, "Esta acción necesita confirmación.")
			return
		}
		var errs []string
		for _, it := range body.Items {
			target := it.Ref
			if target == "" || strings.HasPrefix(target, "<none>") {
				target = it.ID
			}
			err := a.Docker.RemoveImage(r.Context(), target)
			a.Log(userOf(r), target, "Imagen eliminada", err, "")
			if err != nil {
				errs = append(errs, target+": "+translateImgErr(err))
			}
		}
		a.refreshDocker(r.Context(), false)
		if len(errs) > 0 {
			fail(w, 409, strings.Join(errs, " "))
			return
		}
		writeJSON(w, 200, a.Snapshot())
	})

	/* Despliegues */
	auth("POST /api/deploy", func(w http.ResponseWriter, r *http.Request) {
		var s DeploySpec
		if err := readJSON(r, &s); err != nil {
			fail(w, 400, err.Error())
			return
		}
		j, err := a.Deploy(userOf(r), s)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		writeJSON(w, 202, j.copy())
	})

	/* Actualizaciones */
	auth("POST /api/updates/check", func(w http.ResponseWriter, r *http.Request) {
		go a.CheckUpdates()
		time.Sleep(150 * time.Millisecond)
		writeJSON(w, 202, a.Snapshot())
	})
	auth("POST /api/updates/{name}/{action}", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Confirm bool }
		if readJSON(r, &body) != nil || !body.Confirm {
			fail(w, 400, "Esta acción necesita confirmación.")
			return
		}
		var j *Job
		var err error
		switch r.PathValue("action") {
		case "apply":
			j, err = a.ApplyUpdate(userOf(r), r.PathValue("name"), false)
		case "major":
			j, err = a.ApplyUpdate(userOf(r), r.PathValue("name"), true)
		case "revert":
			j, err = a.RevertUpdate(userOf(r), r.PathValue("name"))
		default:
			err = errors.New("Acción desconocida.")
		}
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		writeJSON(w, 202, j.copy())
	})

	/* PC y red */
	auth("POST /api/pc/wake", func(w http.ResponseWriter, r *http.Request) {
		if err := a.Wake(userOf(r), ""); err != nil {
			fail(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, a.Snapshot())
	})
	auth("POST /api/pc/check", func(w http.ResponseWriter, r *http.Request) {
		a.refreshPC()
		writeJSON(w, 200, a.Snapshot())
	})
	auth("PUT /api/network/devices/{mac}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name   string `json:"name"`
			Pinned bool   `json:"pinned"`
		}
		if err := readJSON(r, &body); err != nil {
			fail(w, 400, err.Error())
			return
		}
		if err := a.Net.UpdateDevice(a.Cfg.Get(), r.PathValue("mac"), body.Name, body.Pinned); err != nil {
			fail(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, a.Snapshot())
	})
	auth("POST /api/network/scan", func(w http.ResponseWriter, r *http.Request) {
		if err := a.Net.Scan(a.Cfg.Get()); err != nil {
			fail(w, 503, err.Error())
			return
		}
		a.refreshPC()
		writeJSON(w, 200, a.Snapshot())
	})

	/* Ajustes y automatizaciones */
	auth("PUT /api/config", func(w http.ResponseWriter, r *http.Request) {
		var p PublicConfig
		if err := readJSON(r, &p); err != nil {
			fail(w, 400, err.Error())
			return
		}
		if err := a.Cfg.Update(p); err != nil {
			fail(w, 400, err.Error())
			return
		}
		a.Log(userOf(r), "Ajustes", "Configuración guardada", nil, "")
		a.refreshPC()
		a.Refresh("slow")
		writeJSON(w, 200, a.Snapshot())
	})
	/* Automatizaciones */
	saveRule := func(w http.ResponseWriter, r *http.Request, id string) {
		var rule Rule
		if err := readJSON(r, &rule); err != nil {
			fail(w, 400, err.Error())
			return
		}
		rule.ID = id
		saved, token, err := a.Engine.Save(rule)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		a.Log(userOf(r), saved.Name, "Automatización guardada", nil, "")
		writeJSON(w, 200, map[string]any{"id": saved.ID, "hook": a.hookURLs(r, token), "state": a.Snapshot()})
	}
	auth("POST /api/automations", func(w http.ResponseWriter, r *http.Request) { saveRule(w, r, "") })
	auth("PUT /api/automations/{id}", func(w http.ResponseWriter, r *http.Request) { saveRule(w, r, r.PathValue("id")) })
	auth("POST /api/automations/{id}/enabled", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Enabled bool }
		_ = readJSON(r, &body)
		if err := a.Engine.SetEnabled(r.PathValue("id"), body.Enabled); err != nil {
			fail(w, 404, err.Error())
			return
		}
		writeJSON(w, 200, a.Snapshot())
	})
	auth("POST /api/automations/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Confirm bool }
		if readJSON(r, &body) != nil || !body.Confirm {
			fail(w, 400, "Esta acción necesita confirmación.")
			return
		}
		if err := a.Engine.Delete(r.PathValue("id")); err != nil {
			fail(w, 404, err.Error())
			return
		}
		writeJSON(w, 200, a.Snapshot())
	})
	auth("POST /api/automations/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		if err := a.Engine.RunNow(r.PathValue("id")); err != nil {
			fail(w, 409, err.Error())
			return
		}
		time.Sleep(200 * time.Millisecond)
		writeJSON(w, 200, a.Snapshot())
	})
	auth("POST /api/automations/{id}/hook", func(w http.ResponseWriter, r *http.Request) {
		token, err := a.Engine.NewHook(r.PathValue("id"))
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"hook": a.hookURLs(r, token)})
	})
	// Enlace secreto para Atajos del iPhone, etiquetas NFC u otros sistemas.
	// No usa sesión: el propio enlace es la clave. Solo en la red local y Tailscale.
	open("/api/hooks/{token}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			fail(w, 405, "Usa GET o POST.")
			return
		}
		name, err := a.Engine.Hook(r.PathValue("token"))
		if err != nil {
			code := 409
			if name == "" {
				code = 404
			}
			fail(w, code, err.Error())
			return
		}
		writeJSON(w, 202, map[string]string{"ok": "Lanzada: " + name})
	})

	open("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "Ruta desconocida.") })
	mux.Handle("/", spa(web))
	return a.guard(mux)
}

func (a *App) containerState(name string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, c := range a.docker.Containers {
		if c.Name == name {
			return c.State
		}
	}
	return ""
}

func translateImgErr(err error) string {
	m := err.Error()
	if strings.Contains(m, "being used by") || strings.Contains(m, "conflict") {
		return "la usa un contenedor; elimínalo antes."
	}
	return m
}

// guard limita el acceso a las redes permitidas, añade cabeceras de
// seguridad y exige la cabecera X-Umbral en las peticiones que cambian algo
// (un formulario de otra web no puede enviarla).
func (a *App) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if ip == nil || !a.Cfg.Allowed(ip) {
			http.Error(w, "Umbral solo es accesible desde la red local y Tailscale.", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Umbral") != "1" && !strings.HasPrefix(r.URL.Path, "/api/hooks/") {
			fail(w, 403, "Petición rechazada.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) requireAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		user, ok := "", false
		if err == nil {
			user, ok = a.Auth.Check(c.Value)
		}
		if !ok {
			fail(w, 401, "La sesión ha caducado. Vuelve a iniciar sesión.")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, user)))
	})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		User     string `json:"user"`
		Password string `json:"password"`
		Remember bool   `json:"remember"`
		Code     string `json:"code"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if !a.Auth.Configured() {
		fail(w, 409, "Todavía no hay ningún usuario. Créalo en el servidor con: sudo -u umbral umbral usuario crear <nombre> -datos /var/lib/umbral")
		return
	}
	user := strings.ToLower(strings.TrimSpace(body.User))
	token, ttl, wait, err := a.Auth.Login(clientIP(r).String(), user, body.Password, body.Code, body.Remember)
	if errors.Is(err, ErrNeedCode) {
		writeJSON(w, 401, map[string]any{"error": err.Error(), "needCode": true})
		return
	}
	if errors.Is(err, ErrLocked) {
		a.Log(user, "Inicio de sesión", "Bloqueo temporal", errors.New("demasiados intentos fallidos"), clientIP(r).String())
		a.Access(r, user, "locked")
		fail(w, 429, fmt.Sprintf("Demasiados intentos fallidos. Vuelve a intentarlo en %d min.", int(wait.Minutes())+1))
		return
	}
	if err != nil {
		log.Printf("inicio de sesión fallido para %q desde %s", user, clientIP(r))
		a.Access(r, user, "fail")
		fail(w, 401, err.Error())
		return
	}
	c := &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"}
	if body.Remember {
		c.MaxAge = int(ttl.Seconds())
	}
	http.SetCookie(w, c)
	a.Access(r, user, "login")
	writeJSON(w, 200, map[string]string{"user": user})
}

// spa sirve los archivos del panel y devuelve index.html para cualquier otra ruta.
func spa(web fs.FS) http.Handler {
	files := http.FileServer(http.FS(web))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(web, p); err != nil {
			r.URL.Path = "/"
			p = "index.html"
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

// hookURLs devuelve el enlace secreto para la red local y para Tailscale.
func (a *App) hookURLs(r *http.Request, token string) map[string]string {
	if token == "" {
		return nil
	}
	_, port, _ := net.SplitHostPort(a.Cfg.Get().Listen)
	out := map[string]string{"local": "http://" + r.Host + "/api/hooks/" + token}
	a.mu.RLock()
	if a.tailscale.Self != nil && a.tailscale.Self.DNSName != "" {
		out["tailscale"] = "http://" + a.tailscale.Self.DNSName + ":" + port + "/api/hooks/" + token
	}
	if a.server.IP != "" {
		out["local"] = "http://" + a.server.IP + ":" + port + "/api/hooks/" + token
	}
	a.mu.RUnlock()
	return out
}
