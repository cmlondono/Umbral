# Arquitectura

## Decisiones

| Tema | Decisión | Motivo |
|---|---|---|
| Lenguaje del servicio | **Go**, solo con la biblioteca estándar | Un binario de unos 9 MB que gasta unos 15 MB de RAM, sin nada que instalar en el servidor. |
| Instalación | **Servicio systemd**, no contenedor | Necesita `/proc`, la red del host (Wake-on-LAN, ARP y mDNS) y Tailscale. Un contenedor apenas aislaría más. |
| Base de datos | **Ninguna**: archivos JSON en `/var/lib/umbral`, escritos de forma atómica | Solo guarda configuración, usuarios, sesiones, historial, métricas recientes, dispositivos y automatizaciones. |
| Panel | React, Vite y TypeScript, compilado e incrustado en el binario | Una sola pieza que instalar y actualizar. |
| Métricas | Lectura directa de `/proc` y `/sys` cada 15 s | No hace falta Netdata ni ningún agente. Historial: 1 h a 1 min, 24 h a 15 min y 7 días a 2 h. |

## Qué lee el servicio y cada cuánto

| Fuente | Intervalo | Cómo |
|---|---|---|
| CPU, memoria, discos, red y temperatura | 15 s | `/proc/stat`, `/proc/meminfo`, `/proc/mounts` con `statfs`, `/proc/net/dev`, `/sys/class/hwmon` |
| Vista «En vivo» de las gráficas | 2 s, solo mientras alguien la mira | Lectura ligera de CPU, memoria, red y temperatura, sin discos y sin tocar el historial. Si varias pestañas piden a la vez, comparten la misma lectura. |
| Docker | 15 s | API por el socket, sin SDK. El consumo de cada contenedor solo se mide mientras alguien mira el panel. |
| Nextcloud, Tailscale e Internet | 60 s | `/status.php`, `tailscale status --json` y una conexión TCP a dos DNS públicos |
| PC | 30 s | Conexión TCP al puerto configurado y tabla ARP |
| Red local | 5 min (configurable) | Un datagrama a cada IP de la subred y lectura de `/proc/net/arp`. Nombres por DNS, mDNS y NetBIOS; fabricante por la lista OUI de la IEEE, que se descarga una vez. |
| Actualizaciones de imágenes | 24 h | Petición HEAD al registro para comparar el digest, sin descargar nada |

El botón **Actualizar** del panel fuerza una lectura nueva de todo. En la pantalla Red, también un escaneo de la red.

## Docker

- **Acciones**: iniciar, detener, reiniciar y eliminar contenedores; descargar y eliminar imágenes.
- **Despliegues**:
  - desde el catálogo o desde una imagen, por la API;
  - con Docker Compose, mediante `docker compose`;
  - los secretos se generan y se guardan en `secrets/` (0600).
- **Actualizar un contenedor**:
  - descarga la imagen;
  - comprueba que la versión es compatible;
  - lo recrea con la misma configuración, redes y volúmenes, también los anónimos;
  - si no arranca, vuelve al anterior;
  - funciona igual con contenedores de Compose, sin leer su archivo;
  - la imagen anterior se conserva para poder revertir.
- **Nextcloud tiene un procedimiento propio**:
  - modo de mantenimiento;
  - copia de la base de datos (MariaDB, MySQL o PostgreSQL) en `/srv/umbral/copias`; si falla, no se toca nada;
  - recreación del contenedor y espera a que Nextcloud actualice su base de datos;
  - fin del mantenimiento;
  - bloquea los saltos de más de una versión mayor;
  - si algo sale mal, se queda en mantenimiento para proteger los datos;
  - no se revierte desde el panel, porque la base de datos ya cambió.

## Automatizaciones

Cada regla dice «cuando pase esto, si se cumple esto, haz esto».

**Disparadores:**
- horario;
- enlace secreto (Atajos o NFC);
- un contenedor se detiene, falla o arranca;
- Nextcloud no responde;
- CPU, memoria, disco o temperatura por encima de un umbral;
- un dispositivo llega a la red o se va;
- el PC se enciende o se apaga.

**Condiciones:** franja horaria, días, presencia de un dispositivo, estado del PC y estado de un servicio.

**Acciones:**
- encender el PC;
- iniciar, detener o reiniciar un servicio;
- copia de la base de datos de Nextcloud;
- esperar;
- escanear la red;
- buscar actualizaciones;
- llamar a una URL de la red local o de Tailscale.

**Cómo funciona el motor.** No hace lecturas propias: se evalúa cada 15 s con el estado ya recogido. Para no descontrolarse:
- solo dispara al pasar de inactivo a activo;
- respeta la duración mínima;
- se rearma con un margen en los umbrales;
- no dispara al arrancar por situaciones que ya existían;
- no actúa sobre un contenedor que el panel está actualizando;
- respeta una pausa entre ejecuciones;
- ejecuta como mucho dos reglas a la vez.

**Enlaces secretos:** en disco solo se guarda su hash, se limitan a una petición cada 10 s y solo responden en las redes permitidas.

**Ampliación prevista:** el diseño admite añadir canales de aviso (por ejemplo, Telegram o ntfy) como un tipo de acción más, sin cambiar el motor.

## Seguridad

- **Acceso**: solo desde las redes de `allowNets` (red local, Tailscale y el propio equipo).
- **Usuarios**: se crean por terminal, nunca desde la web.
- **Contraseñas**: PBKDF2-SHA256 con 600.000 iteraciones y al menos 10 caracteres. Tras 5 fallos desde un mismo equipo, bloqueo de 15 minutos.
- **Verificación en dos pasos (TOTP, RFC 6238)**:
  - se activa desde la cuenta, con la contraseña y un código de confirmación;
  - un código no se acepta dos veces;
  - hay 8 códigos de recuperación de un solo uso, guardados como hash;
  - activarla o cambiar la contraseña cierra las demás sesiones.
- **Sesión**: cookie aleatoria `HttpOnly` y `SameSite=Strict`, de 12 horas o 30 días. En disco solo se guarda su hash.
- **Peticiones**: las que cambian algo exigen la cabecera `X-Umbral`; las destructivas, además, `confirm: true`. Cabeceras CSP estrictas, `X-Frame-Options: DENY` y `no-referrer`.
- **Sin órdenes libres**: las acciones son una lista cerrada y validada.
- **systemd**: usuario propio, sin privilegios, sistema de solo lectura salvo `/var/lib/umbral` y `/srv/umbral`, y como mucho 128 MB de memoria.

**Límites conocidos:**
- En la red local va por HTTP sin cifrar. Se recomienda `tailscale serve` para tener HTTPS.
- El grupo `docker` equivale en la práctica a acceso de administrador.
- Las copias de seguridad están en el mismo disco.

## Lo que todavía no hace

| Función | Qué falta |
|---|---|
| Varios dispositivos con Wake-on-LAN y acceso remoto | Hoy solo hay un PC registrado. |
| Avisos al móvil | Un canal de avisos como acción de las automatizaciones. |
| Bloquear dispositivos de la Wi‑Fi | Un router con API o firmware compatible (OpenWrt, MikroTik, UniFi u Omada). |
| Apagar o reiniciar el PC | Un agente en el PC. |
| Construir imágenes desde un Dockerfile | Subir archivos al servidor desde el panel. |
| Otros idiomas | El panel está solo en español. |
