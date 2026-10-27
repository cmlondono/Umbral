// Umbral: panel de control del servidor y la red de casa.
//
//	umbral serve                          arranca el servicio
//	umbral usuario crear <nombre>         crea un usuario o cambia su contraseña
//	umbral usuario borrar <nombre>        elimina un usuario y cierra sus sesiones
//	umbral usuario lista                  muestra los usuarios
//	umbral usuario quitar-2fa <nombre>    quita el doble factor (si se perdió el móvil)
//
// Opción común: -datos /var/lib/umbral (carpeta de configuración y estado).
package main

import (
	"bufio"
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"umbral/server/internal/core"
)

//go:embed all:web/dist
var webFS embed.FS


func main() {
	fl := flag.NewFlagSet("umbral", flag.ExitOnError)
	dataDir := fl.String("datos", "/var/lib/umbral", "carpeta de configuración y estado")
	// Las opciones pueden ir en cualquier posición: se separan de las palabras.
	var opts, words []string
	for a := os.Args[1:]; len(a) > 0; a = a[1:] {
		if strings.HasPrefix(a[0], "-") {
			opts = append(opts, a[0])
			if !strings.Contains(a[0], "=") && len(a) > 1 {
				opts, a = append(opts, a[1]), a[1:]
			}
		} else {
			words = append(words, a[0])
		}
	}
	_ = fl.Parse(opts)
	cmd, rest := "serve", []string{}
	if len(words) > 0 {
		cmd, rest = words[0], words[1:]
	}

	switch cmd {
	case "serve":
		serve(*dataDir)
	case "usuario":
		if err := users(*dataDir, rest); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println("Umbral", core.Version)
	default:
		fmt.Fprintln(os.Stderr, "Uso: umbral [serve | usuario crear|borrar|lista|quitar-2fa <nombre> | version] [-datos carpeta]")
		os.Exit(2)
	}
}

func serve(dataDir string) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		log.Fatal(err)
	}
	app, err := core.NewApp(dataDir)
	if err != nil {
		log.Fatal(err)
	}
	if !app.Auth.Configured() {
		log.Println("Aviso: no hay ningún usuario. Créalo con: sudo -u umbral umbral usuario crear <nombre> -datos " + dataDir)
	}
	web, _ := fs.Sub(webFS, "web/dist")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go app.Run(ctx)

	srv := &http.Server{Addr: app.Cfg.Get().Listen, Handler: app.Handler(web), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Printf("Umbral %s escuchando en %s (datos en %s)", core.Version, srv.Addr, dataDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	app.Hist.Save()
}

func users(dataDir string, args []string) error {
	if len(args) == 0 {
		return errors.New("indica crear, borrar o lista")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	auth, err := core.NewAuth(dataDir)
	if err != nil {
		return err
	}
	switch args[0] {
	case "lista":
		for _, u := range auth.Users() {
			fmt.Println(u)
		}
		return nil
	case "quitar-2fa":
		if len(args) < 2 {
			return errors.New("indica el nombre del usuario")
		}
		if err := auth.ResetTOTP(args[1]); err != nil {
			return err
		}
		fmt.Println("Doble factor quitado. Entra con la contraseña y vuelve a activarlo en Ajustes › Seguridad.")
		return nil
	case "borrar":
		if len(args) < 2 {
			return errors.New("indica el nombre del usuario")
		}
		return auth.DeleteUser(args[1])
	case "crear":
		if len(args) < 2 {
			return errors.New("indica el nombre del usuario")
		}
		p1, err := readPassword("Contraseña (mínimo 10 caracteres): ")
		if err != nil {
			return err
		}
		p2, err := readPassword("Repite la contraseña: ")
		if err != nil {
			return err
		}
		if p1 != p2 {
			return errors.New("las contraseñas no coinciden")
		}
		if err := auth.SetPassword(strings.ToLower(args[1]), p1); err != nil {
			return err
		}
		fmt.Println("Usuario guardado. Ya puedes iniciar sesión en el panel.")
		return nil
	}
	return errors.New("orden desconocida: " + args[0])
}

var stdin = bufio.NewReader(os.Stdin)

// readPassword lee la contraseña sin mostrarla, apagando el eco del terminal
// con stty. Si no hay terminal (por ejemplo, en un script), la lee tal cual.
func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	restore := noEcho()
	line, err := stdin.ReadString('\n')
	restore()
	fmt.Fprintln(os.Stderr)
	if err != nil && line == "" {
		return "", errors.New("no se pudo leer la contraseña")
	}
	return strings.TrimRight(line, "\r\n"), nil
}
