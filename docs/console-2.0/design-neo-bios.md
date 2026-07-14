# Console 2.0 — Neo-BIOS Operations Firmware (design handoff)

Status: **design proposal** for milestone P8.1 (Console 2.0). Not implemented.
Prepared on a separate branch so it does not touch `main` or `p9-brain`.
The reference mockup lives beside this file: `mockup.html` (self-contained, no build).

## Intent

Rediseñar la consola P8 (presentation-only) para que su identidad visual nazca de
una **BIOS Award/Phoenix** de principios de los 2000 — no una parodia CRT, sino un
"firmware operativo" moderno — y prepararla para evolucionar a un **operator cockpit**
de solo observación y control, sin convertirse en otro cliente de IA.

La inteligencia sigue viviendo en el cliente (ChatGPT, Claude Code, opencode). La
consola aporta observación, aprobación y control humano. No incrusta ningún modelo.

## Design language (fijo)

- **Fondo azul VGA** `#0000A8`, sin gradientes ni glassmorphism ni tarjetas redondeadas.
- Tipografía monoespaciada; `border-radius: 0` en todo.
- **Paleta VGA 16**: texto blanco `#F8F8F8`, valores amarillo `#FCFC54`, secciones y
  enlaces cian `#54FCFC`, estados verde `#54FC54` / ámbar `#FCA854` / rojo `#FC5454`,
  grises `#A8A8A8 / #545454`. Barras y diálogos con **sombra dura negra** desplazada.
- Layout de setup: barra superior, pestañas, cuerpo **lista + panel "Item Specific Help"**
  a la derecha que cambia con el ítem seleccionado, y **barra de teclas F** al pie.
- **Un solo tema** (azul BIOS) por decisión estética; no se ofrece modo claro.

## Interacción

- Teclado real: `←→` pantalla, `↑↓` ítem (actualiza la ayuda contextual, escrita a
  máquina), `P` cambia de proyecto, `F1` ayuda, `F5` refresh, `F8` modo attract (demo),
  `F9/F10` cancelar/aprobar, `ESC/Enter` cierran diálogos. Todo con `preventDefault`
  para que el navegador no capture F1/F5. Usable también con ratón/táctil.
- Animación de "pintado" secuencial al cambiar de pestaña + tipeo de la ayuda.
  Todo se desactiva con `prefers-reduced-motion`.

## Pestañas

- **System** — identidad de runtime, *active operation en vivo*, recursos de la VPS
  (RAM/disco con medidores de bloques), delivery stages.
- **Agents** — session ledger multi-agente (quién, auth, repo, operación, estado) +
  Payload I/O estimado + reglas de concurrencia (lock por repo, sesiones con ámbito).
- **Tasks** — **Task Journal durable**: timeline de estados
  `requested→planned→approved→executing→observing→validating→completed/failed/cancelled/disconnected`
  y tabla de tareas con heartbeat. Vía SSE, sin servicios residentes.
- **Brain** — memory map (curated/working, FTS5, 0 MB idle) e invariantes anti-deriva.
- **Graph** — grafo de enlaces del brain en canvas: **zoom con rueda, pan arrastrando,
  tooltip con resumen al hover**; nodos como círculos (tamaño = grado), títulos que
  aparecen al acercar. Amarillo=curated, blanco=working, gris=huérfana.
- **Edge** — dispositivos (outbound-only), installer/usuario no-root/root-helper de
  perfil cerrado/roots por alias/engagement HTB/límites de workcell. Todo marcado
  `[Planned]` / `[Not paired]` — **no se inventan datos**.
- **Observability** — eventos por clase de ruta (contrato P7): solo clases y contadores.
- **Security** — invariantes, estado de autenticación (OAuth primary, bearer kept,
  `?key=` a eliminar), gates de CI/CD y Vulnerability Ledger.
- **Events** — log estilo POST.

## Data contract (importante para no romper P8)

Los datos DINÁMICOS de la consola deben salir del schema público allowlisted de
`/console/status`. Las secciones nuevas (recursos, observability, task journal,
tokens estimados) **requieren ampliar el contrato con endpoints seguros nuevos**,
cada uno con su propia allowlist y test que enumere las claves exactas. Ninguna de
estas superficies puede exponer repos, rutas, prompts, params, resultados, tokens,
IPs ni razonamiento privado del modelo. El estimado de tokens = bytes de payload MCP
/ 4 (el servidor nunca ve los tokens reales del modelo).

## Progressive enhancement opcional (no dependencia)

El resumen de un nodo del grafo puede enriquecerse con **IA on-device del navegador**
(Chrome Prompt/Summarizer API, Gemini Nano) SOLO como mejora progresiva: si la API
existe, resume las notas enlazadas; si no, se muestra el resumen estático. Nunca es
una dependencia y nunca envía datos a un servicio externo (rompería el CSP y la tesis).

## Restricciones que se conservan de P8

- Presentation/observation-only: la consola no ejecuta tools ni acciones consecuentes;
  las aprobaciones se disparan hacia el flujo de planes TTL del MCP, no se inventan.
- CSP sin script inline (el JS va en un `app.js` embebido), cabeceras de endurecimiento,
  cookie opaca `HttpOnly+SameSite=Strict`, sesiones digest-only en memoria.
- El contrato público de 62 tools no cambia.
