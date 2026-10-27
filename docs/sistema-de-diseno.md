# Sistema de diseño: Apple y Liquid Glass

El CSS está en `src/design/`, por capas: `tokens.css`, `shell.css`, `components.css`, `screens.css`, `layers.css`, `responsive.css`, `glass.css`, `deploy.css` y `login.css`. Los componentes base están en `src/design/ui.tsx`.

## Tipografía

- Fuentes, en este orden: `-apple-system, BlinkMacSystemFont, 'SF Pro Text', 'SF Pro Display', 'Inter', system-ui, sans-serif`. Inter se carga desde Google Fonts solo como alternativa.
- Tamaños:
  - Título de página: 34 px, peso 700, interletrado −0,025 em (30 y 28 px en pantallas pequeñas).
  - Título de sección: 17 px, peso 600.
  - Texto: 14 px.
  - Secundario: 12,5–13 px.
- Cifras tabulares (`font-variant-numeric: tabular-nums`). Monoespaciada (`ui-monospace, 'SF Mono'`) solo para imágenes, nombres técnicos y YAML.
- Todo en mayúscula y minúsculas normales («Sentence case»). Nunca etiquetas en mayúsculas.

## Color

| Token | Claro | Oscuro | Uso |
|---|---|---|---|
| `--ink` | #1d1d1f | #f5f5f7 | Texto |
| `--ink2` | #424245 | #c7c7cc | Texto secundario |
| `--ink3` | #6e6e73 | #98989d | Texto terciario |
| `--accent` | #0071e3 | #0a84ff | Acción y selección |
| `--ok` | #248a3d | #30d158 | Correcto |
| `--warn` | #c93400 | #ff9f0a | Aviso |
| `--err` | #d70015 | #ff453a | Problema |
| `--off` | #8e8e93 | #8e8e93 | Apagado o desconocido |
| `--fill` | rgba(118,118,128,.12) | .24 | Fondos de controles |
| `--line` | rgba(60,60,67,.13) | rgba(84,84,88,.5) | Separadores |
| `--card` | rgba(255,255,255,.78) | rgba(28,28,30,.70) | Material del contenido |

**Fondo ambiental claro** (`--wall`): tres manchas radiales muy suaves (azul cielo arriba a la izquierda, salvia a la derecha y arena abajo) sobre #e6e9ee, fijas al desplazarse. Con el fondo liso se usa #f2f2f7 (o #000 en modo oscuro).

## Material Liquid Glass (`.glass`)

```css
background: linear-gradient(180deg, var(--gTop), var(--gBot));   /* blanco .62 → .30 */
backdrop-filter: blur(14px) saturate(210%) brightness(1.04);
box-shadow:
  inset 0 1px 1px var(--gHi),      /* brillo especular superior */
  inset 0 -1px 1px var(--gLow),
  inset 0 0 16px var(--gGlow),     /* brillo interior en los bordes */
  0 0 0 .5px rgba(0,0,0,.06),
  0 12px 34px rgba(20,28,40,.14), 0 2px 6px rgba(20,28,40,.08);
/* ::before → borde de luz diagonal de 1 px, con un degradado enmascarado (mask-composite: exclude) */
```

- **Variante densa** para las hojas, los modales, los avisos y el asistente: blanco .80 → .66, con desenfoque de 30 px.
- **Dónde va el cristal**: solo en la navegación y en lo que flota. El contenido usa `--card` con desenfoque de 30 px.
- **Pestaña seleccionada**: una «gota» más clara (`--gSel`), con el texto en color de acento y una leve escala al pulsar.
- **Transparencia reducida**: cambia todo el cristal y el material por `--surface` opaco y quita el borde de luz. Se activa con el ajuste o con `prefers-reduced-transparency`.

## Forma

- Tarjetas: 22 px. Elementos internos: 14 px. Iconos en caja: 10 px. Hojas y modales: 28–32 px. Barra de pestañas del móvil: 32 px.
- Botones, controles segmentados, búsqueda y etiquetas: cápsula (999 px).
- Separadores de lista: empiezan después del icono (padding más 46 px).

## Componentes

- **StatusSymbol**: círculo relleno del color del estado con un glifo blanco (negro en modo oscuro), de 16 px. Va siempre junto al texto. Glifos disponibles: check, x, menos, interrogación, giratorio (animado), alerta, puntos (pendiente), matraz (simulado), anillo (detectado) y flecha (actualización).
- **Badge**: StatusSymbol más texto. El texto es neutro salvo en los errores, que van en rojo.
- **Button**: con estas variantes:
  - Normal: fondo gris y texto de acento.
  - Principal: relleno de acento.
  - Peligro: texto rojo. Peligro sólido: relleno rojo, solo dentro de las confirmaciones.
  - Silencioso.
  - **Pendiente**: borde discontinuo y texto gris. Al pulsarlo explica qué falta.
- **Discontinuo significa siempre «pendiente, simulado o futuro»**, ya sea en botones, etiquetas, líneas del mapa o insignias.
- **Segmented**, **Switch** (46×28, verde #34c759), **Search**, **Field** (con mensajes de validación en rojo), **Chip**, **Meter**, **ListRow**, **Table**, **Panel**, **Drawer**, **Modal**, **Toast**, **Sheet** y **Wizard** (barra de progreso de cuatro tramos).

## Movimiento

- Animaciones cortas: 160–260 ms, curva `cubic-bezier(.2,.8,.2,1)`.
- Solo cuando algo cambia o responde a una acción: aparición de hojas, modales y avisos, giro de los estados en curso, y escala de las pestañas al pulsar.
- Se respeta `prefers-reduced-motion`.

## Escritura

- Los botones dicen lo que ocurrirá: «Desplegar servicio», «Eliminar imagen», «Encender con Wake-on-LAN».
- Las acciones bloqueadas dicen por qué: «No disponible: el servidor no responde».
- Los errores dicen qué pasó y cómo arreglarlo: «El puerto 8080 ya lo usa nextcloud-app.» junto a «Usar 8081».
- Las acciones simuladas lo dicen: «Despliegue simulado completado. No se ha creado ningún contenedor».
