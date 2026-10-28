# Arquitectura

## Decisiones

| Tema | Decisión | Motivo |
|---|---|---|
| Lenguaje del servicio | **Go**, solo con la biblioteca estándar | Un binario de unos 9 MB que gasta unos 15 MB de RAM, sin nada que instalar en el servidor. |
| Instalación | **Servicio systemd**, no contenedor | Necesita `/proc`, la red del host (Wake-on-LAN, ARP y mDNS) y Tailscale. Un contenedor apenas aislaría más. |
| Base de datos | **Ninguna**: archivos JSON en `/var/lib/umbral`, escritos de forma atómica | Solo guarda configuración, usuarios, sesiones, historiales, métricas recientes, dispositivos y automatizaciones: unos cientos de registros. |
| Panel | React, Vite y TypeScript, compilado e incrustado en el binario | Una sola pieza que instalar y actualizar. Se puede instalar como app en el móvil (manifiesto web e icono), sin service worker ni caché que pueda quedarse vieja. |
| Métricas | Lectura directa de `/proc` y `/sys` cada 15 s | No hace falta ningún agente. Historial: 1 h a 1 min, 24 h a 15 min y 7 días a 2 h. |
| Tareas de root | Un **ayudante** aparte (`umbral sistema`) que lanza systemd | El servicio nunca es root. El ayudante no recibe datos del panel y siempre hace lo mismo. |

## Qué lee el servicio y cada cuánto

| Fuente | Intervalo | Cómo |
|---|---|---|
| CPU, memoria, discos, red y temperatura | 15 s | `/proc/stat`, `/proc/meminfo`, `/proc/mounts` con `statfs`, `/proc/net/dev`, `/sys/class/hwmon` |
| Vista «En vivo» de las gráficas | 2 s, solo mientras alguien la mira | Lectura ligera de CPU, memoria, red y temperatura, sin discos y sin tocar el historial. Si varias pestañas piden a la vez, comparten la misma lectura. |
| Docker | 15 s | API por el socket, sin SDK. El consumo de cada contenedor solo se mide mientras alguien mira el panel. |
| Disponibilidad de los servicios | 15 s | Con el estado de Docker ya leído. Cubos de una hora durante 7 días y las 100 últimas caídas. |
| Nextcloud, Tailscale, router e Internet | 60 s | `/status.php`, `tailscale status --json` y `tailscale serve status --json`, la API del router y una conexión TCP a dos DNS públicos |
| PC | 30 s | Conexión TCP al puerto configurado y tabla ARP |
| Red local | 5 min (configurable) | Un datagrama a cada IP de la subred y lectura de `/proc/net/arp`. Nombres por DNS, mDNS, NetBIOS y el DHCP del router; fabricante por la lista OUI de la IEEE, que se descarga una vez. |
| Actualizaciones de imágenes | 24 h | Petición HEAD al registro para comparar el digest, sin descargar nada |
| Actualizaciones de Debian | 6 h | `apt-get -s dist-upgrade`, que solo simula y no necesita root |
| Salud de los discos y lista de paquetes | 6 h, o al pulsar «Comprobar ahora» | El ayudante de root: `smartctl --json -a` y `apt-get update` |

El botón **Actualizar** del panel fuerza una lectura nueva de todo. En la pantalla Red, también un escaneo de la red.

## Docker

- **Acciones**: iniciar, detener, reiniciar y eliminar contenedores; descargar y eliminar imágenes.
- **Registro de cada contenedor**: las últimas 200, 500 o 2000 líneas, con filtro, solo la salida de errores y copiar.
- **Límite de memoria**: se pone o se cambia al momento con `POST /containers/{id}/update`. Docker no permite quitarlo así, de modo que para quitarlo se recrea el contenedor con la misma imagen exacta y los mismos volúmenes.
- **Espacio**: calcula con `/system/df` la caché de compilación y los volúmenes que ningún contenedor usa, ni siquiera detenido. Se libera la caché o se elimina cada volumen, siempre con confirmación.
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
  - Si falla y luego se arregla a mano, el aviso desaparece solo en cuanto el contenedor funciona; el fallo queda en el historial.
- **Versiones mayores de Nextcloud**: si el contenedor usa una etiqueta de versión fija (`30`, `30-apache`, `30.0.2-fpm`…), el servidor comprueba con una petición HEAD si ya existe la siguiente (`31-apache`).
  - El panel ofrece el salto con el mismo procedimiento que el resto de actualizaciones.
  - Antes exige aplicar la actualización pendiente de la versión actual.
  - Si el contenedor viene de Docker Compose, avisa de cambiar también la etiqueta en su archivo.
- **Nextcloud tiene un procedimiento propio**:
  - modo de mantenimiento;
  - copia de la base de datos (MariaDB, MySQL o PostgreSQL) en `<datos>/copias`; si falla, no se toca nada;
  - recreación del contenedor y espera a que Nextcloud actualice su base de datos;
  - fin del mantenimiento;
  - bloquea los saltos de más de una versión mayor;
  - si algo sale mal, se queda en mantenimiento para proteger los datos;
  - no se revierte desde el panel, porque la base de datos ya cambió.

## Copias de seguridad

- Las de la base de datos de Nextcloud se guardan en `<datos>/copias`, las 5 últimas.
- Se pueden **descargar** desde el panel.
- Se pueden **copiar a una carpeta externa**, normalmente un disco USB montado en `/media` o `/mnt`:
  - systemd solo deja escribir a Umbral en esas dos rutas, y solo donde el usuario `umbral` tenga permiso;
  - el panel comprueba que la carpeta existe, que se puede escribir y que no está en el mismo disco;
  - allí se guardan las 10 últimas;
  - se hace a mano o con la acción «Copiar las copias al disco externo» de las automatizaciones.
- **Copia completa de Nextcloud** (base de datos y archivos de los usuarios) al disco externo:
  - Los archivos son de `www-data` y Umbral no puede leerlos. La copia la hace un contenedor temporal con la propia imagen de Nextcloud, que trae `rsync`:
    - con los volúmenes de Nextcloud en solo lectura y la carpeta externa montada;
    - sin red;
    - se borra al terminar y no aparece en la lista de servicios.
  - Cada copia es una instantánea completa en `<externa>/nextcloud-archivos/<fecha>`. Los archivos que no cambiaron son enlaces duros a la anterior (`rsync --link-dest`): solo ocupa espacio lo nuevo. Por eso el disco externo debe ser ext4, no exFAT ni NTFS, y el panel lo comprueba antes.
  - Copia `config`, `custom_apps`, `themes` y la carpeta de datos (la que indique `occ config:system:get datadirectory`), sin las miniaturas, que Nextcloud regenera.
  - Guarda las 7 últimas (configurable) y, si se elige, pone Nextcloud en mantenimiento durante la copia.
  - Restaurar es manual, con la guía del panel: un clic equivocado sobrescribiría los datos de todos.

## Automatizaciones

Cada regla dice «cuando pase esto, si se cumple esto, haz esto».

**Disparadores:**
- horario;
- enlace secreto (Atajos o NFC);
- un contenedor se detiene, falla o arranca;
- Nextcloud no responde;
- CPU, memoria, disco o temperatura por encima de un umbral;
- un dispositivo llega a la red o se va;
- el PC se enciende o se apaga;
- acceso al panel: bloqueo por intentos fallidos, entrada desde un equipo nuevo o cualquier entrada.

**Condiciones:** franja horaria, días, presencia de un dispositivo, estado del PC y estado de un servicio.

**Acciones:**
- avisar al móvil (ntfy);
- encender el PC;
- iniciar, detener o reiniciar un servicio;
- copia de la base de datos de Nextcloud;
- copia completa de Nextcloud (base de datos y archivos) al disco externo;
- copiar las copias al disco externo;
- esperar;
- escanear la red;
- buscar actualizaciones;
- llamar a una URL de la red local o de Tailscale.

**Cómo funciona el motor.** No hace lecturas propias: se evalúa cada 15 s con el estado ya recogido; los accesos al panel lo avisan al momento. Para no descontrolarse:
- solo dispara al pasar de inactivo a activo;
- respeta la duración mínima;
- se rearma con un margen en los umbrales;
- no dispara al arrancar por situaciones que ya existían;
- no actúa sobre un contenedor que el panel está actualizando;
- respeta una pausa entre ejecuciones;
- ejecuta como mucho dos reglas a la vez.

**Enlaces secretos:** en disco solo se guarda su hash, se limitan a una petición cada 10 s y solo responden en las redes permitidas.

**Avisar si falla:** cada regla puede enviar un aviso al móvil cuando una de sus acciones falla.

**Avisos:** se envían a un servidor ntfy (el público ntfy.sh o uno propio) con un tema difícil de adivinar. El token, si lo hay, vive en `secrets/notify.json` y nunca llega al navegador. Otros canales, como Telegram, serían otro tipo de canal con el mismo `Send`.

## Router

Solo con routers que tienen API. Cada modelo es un controlador con las mismas operaciones:

| Router | API | Requisitos |
|---|---|---|
| OpenWrt 21 o posterior | ubus por JSON-RPC (`/ubus`), la misma que usa LuCI | Usuario root. `luci-mod-rpc` para leer el DHCP. |
| MikroTik RouterOS 7 | REST (`/rest`) | Servicio `www-ssl` o `www` y un usuario con read, write, reboot y rest-api |

**Qué hace:**
- lee modelo, versión y tiempo encendido;
- toma los nombres del DHCP para los equipos que no anuncian el suyo;
- quita o devuelve Internet a un equipo por su MAC:
  - en OpenWrt, con una regla `REJECT` de LAN a WAN llamada `umbral-bloqueo-…`;
  - en MikroTik, con una regla `drop` en `forward`, la primera de la cadena, comentada `umbral-bloqueo`;
- reinicia el router.

La contraseña vive en `secrets/router.json`. Si el router usa un certificado propio, hay que aceptarlo expresamente.

## Publicar con HTTPS en Tailscale

`tailscale serve --bg --https=<puerto> http://127.0.0.1:<puerto>` publica Umbral o cualquier puerto de un contenedor con un certificado válido, solo para los equipos de la tailnet. Nunca se usa Funnel, que lo abriría a Internet.

Detrás de ese proxy, Umbral toma la IP real de la última entrada de `X-Forwarded-For`, solo cuando la petición llega desde el propio servidor. Así el registro de accesos y el bloqueo por intentos siguen funcionando por equipo.

## Ayudante de sistema

`umbral-sistema.service` ejecuta `umbral sistema` como root:
- cada 6 h, con `umbral-sistema.timer`;
- cuando el panel escribe `/var/lib/umbral/helper-request`, con `umbral-sistema.path`; como mucho, una vez cada 5 min.

**Qué hace, siempre lo mismo y sin leer nada del panel:**
- SMART de cada disco, si `smartmontools` está instalado;
- `apt-get update`.

Escribe los resultados en `/var/lib/umbral-helper`, una carpeta de root que Umbral solo puede leer, con nombres temporales aleatorios y renombrado. Así root nunca sigue un enlace simbólico ajeno.

## Seguridad

- **Acceso**: solo desde las redes de `allowNets` (red local, Tailscale y el propio equipo).
- **Usuarios**: se crean por terminal, nunca desde la web.
- **Contraseñas**: PBKDF2-SHA256 con 600.000 iteraciones y al menos 10 caracteres. Tras 5 fallos desde un mismo equipo, bloqueo de 15 minutos.
- **Verificación en dos pasos (TOTP, RFC 6238)**:
  - se activa desde la cuenta, con la contraseña y un código de confirmación;
  - un código no se acepta dos veces;
  - hay 8 códigos de recuperación de un solo uso, guardados como hash;
  - activarla o cambiar la contraseña cierra las demás sesiones.
- **Registro de accesos**: entradas, intentos fallidos, bloqueos, salidas y cambios de seguridad, con IP y navegador. Se guardan los 300 últimos y marca la primera entrada desde cada IP.
- **Sesión**: cookie aleatoria `HttpOnly` y `SameSite=Strict`, de 12 horas o 30 días. En disco solo se guarda su hash.
- **Peticiones**: las que cambian algo exigen la cabecera `X-Umbral`; las destructivas, además, `confirm: true`. Cabeceras CSP estrictas, `X-Frame-Options: DENY` y `no-referrer`.
- **Sin órdenes libres**: las acciones son una lista cerrada y validada.
- **Secretos** (avisos, router y despliegues): en `/var/lib/umbral/secrets`, con permisos 0600. El panel solo sabe si existen.
- **systemd**:
  - usuario propio, sin privilegios, y como mucho 128 MB de memoria;
  - sistema de solo lectura salvo `/var/lib/umbral`, `/srv/umbral`, y `/media` y `/mnt` para las copias externas.

**Límites conocidos:**
- En la red local va por HTTP sin cifrar. Se recomienda publicarlo con HTTPS en Tailscale desde Acceso remoto.
- El grupo `docker` equivale en la práctica a acceso de administrador.

## Lo que todavía no hace

| Función | Qué falta |
|---|---|
| Varios dispositivos con Wake-on-LAN y acceso remoto | Hoy solo hay un PC registrado. |
| Otros routers (UniFi, Omada, Fritz!Box…) | Un controlador para su API. |
| Apagar o reiniciar el PC | Un agente en el PC. |
| Instalar actualizaciones de Debian desde el panel | A propósito: necesita root y conviene hacerlo a mano. |
| Construir imágenes desde un Dockerfile | Subir archivos al servidor desde el panel. |
| Otros idiomas | El panel está solo en español. |
