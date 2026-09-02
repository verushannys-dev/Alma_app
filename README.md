# ALMA APP — Orquestador

Estructura del orquestador + módulos reciclables, en Go, monolito modular.
Compilado, verificado con `go build ./...` y `go vet ./...`, y con tests
(`go test ./...`) que prueban las reglas de negocio reales, no solo que
el código compile.

## Decisiones de arquitectura tomadas

- **Monolito modular**: orquestador y módulos se comunican por interface Go
  (`modules.Module`), no por HTTP.
- **Clasificador detrás de interface** (`classifier.Classifier`): hoy corre
  contra Claude API, pensado para migrar a Ollama más adelante.
- **Niveles de acceso como string, no número** (`"1"`, `"2"`, `"2p"`, `"3"`):
  la línea afiliado y la línea prestador son escalas distintas, no
  comparables — ver `types.NivelAfiliadoAlcanza` y spec_orquestador.md
  sección 4a.
- **Defensa en profundidad real, no solo documentada**: cada módulo
  (`turnos.go`, `sedes.go`) revalida su propio `AccessLevelRequired()`
  contra `ModuleInput.SessionAccessLevelActual` — nunca contra
  `AccessLevelRequired` de arriba, que es una opinión del clasificador
  (un LLM), no un hecho. Ver test `TestDefensaEnProfundidad_...`.
- **Turnos no duplica turnos**: antes de ofrecer franjas o crear uno
  nuevo, `solicitar()` consulta si el afiliado ya tiene un turno vigente
  de esa especialidad (`AgendaClient.BuscarTurnoVigente`). Si ya lo tiene,
  informa y ofrece anular/reprogramar en vez de seguir.
- **Beneficiario, implementado**: el turno puede ser para el afiliado que
  escribe o para cualquier integrante de su grupo familiar
  (`params["beneficiario_dni"]`, default al DNI de la sesión si no se
  especifica). "¿Ya tiene vigente?" y el límite de 3 (siguiente punto)
  cruzan especialidad + beneficiario, no solo especialidad.
- **Límite de 3 turnos activos por persona, implementado**
  (`AgendaClient.ContarTurnosActivos`, constante `LimiteTurnosActivos`).
  Por persona, no compartido entre el grupo familiar.
- **El afiliado elige la franja horaria, no se le asigna automáticamente**
  — confirmado contra el comportamiento real del bot de Botmaker.
- **Sedes filtra por servicio, implementado** (spec_sedes.md sección 4a):
  si la sede pedida no ofrece la especialidad, se buscan alternativas en
  la misma zona; si ninguna sede de la zona la tiene, se informa y se
  pregunta antes de ampliar a otra zona — nunca de oficio. Cuando Turnos
  usa una sede distinta a la pedida, el mensaje de franjas avisa el
  cambio explícitamente (antes esto no pasaba — un hallazgo real al
  escribir el test).
- **Catálogos cargados desde JSON**: `sedes.CargarCatalogo` y
  `turnos.CargarAgendaFixture` leen `data/sedes.json`,
  `data/especialidades.json` y `data/agenda_fixture.json` — ya no hay
  arrays hardcodeados en `main.go`. El prompt del clasificador se arma
  citando `especialidades.json`, para que siempre devuelva el mismo `id`.
- **Multi-intención del mismo dominio, implementado**: `execution_order`
  ahora referencia `id` de intención (no `domain`), y `ConversationState.Pending`
  pasa a ser una lista — dos intenciones de `turnos` en el mismo mensaje
  (ej. "odontología para mí y cardiología infantil para mi hijo") se
  resuelven ambas de forma independiente, y si una queda a mitad de
  resolver no bloquea a la otra ya confirmada
  (`TestDosIntencionesMismoDominio_SeResuelvenAmbas`,
  `TestDosIntencionesMismoDominio_UnaPendienteNoBloqueaALaOtra`).
- **`cambiar()` ya usa la sede/especialidad del turno original como
  default** cuando el afiliado no especifica una nueva.

## ⚠️ Gaps pendientes — explícitos a propósito, no mockeados como si funcionaran

- **Bug corregido el 28/08/2026, encontrado en la primera corrida real
  contra la API** (`claude.go`): dos problemas, no uno. (1) `MaxTokens`
  estaba en 1000 — se quedaba corto con dos intenciones o una
  `clarification` larga, cortando el JSON a mitad de camino (`unexpected
  end of JSON input`). Subido a 2000. (2) El código nunca implementaba el
  `TODO` que ya anticipaba que Claude a veces envuelve el JSON en
  backticks de markdown pese a que el prompt le pide no hacerlo — ahora
  `limpiarBackticksMarkdown()` lo saca antes de intentar parsear. De 17
  casos de prueba, 16 fallaban por esto — ninguno era un problema de
  lógica de negocio, los dos eran de cómo se procesaba la respuesta cruda.

- **Cambio de diseño pendiente (27/08/2026, sin implementar): confirmar
  la sede alternativa ANTES de mostrar franjas, no en el mismo mensaje.**
  Hoy, cuando la sede pedida no tiene el servicio, `resolverSede` +
  `ofrecerFranjas` mandan todo junto: "Luján no tiene cardiología, te
  cambio a Hiper Libertad" + la lista de franjas, en un solo
  `ModuleOutput`. La regla nueva (decidida, no implementada): informar la
  alternativa y **esperar confirmación** ("¿te interesa esa sede?") antes
  de consultar y mostrar franjas — evita gastar la consulta de franjas si
  el afiliado no quiere ir a esa sede (elige mayormente por cercanía
  geográfica). Como beneficio adicional, esto resuelve de forma natural
  el bug de abajo (2+ alternativas) — el paso de confirmación pasa a
  existir siempre, no como caso especial. Afecta: `spec_turnos.md`,
  `spec_sedes.md`, `turnos.go` (`resolverSede`/`solicitar`), sus tests, y
  el material de presentación (`casos_uso_tecnicos_alma_app.docx`,
  `casos_uso_interactivo.html`) — ninguno de estos se tocó todavía.
- **Bug encontrado el 27/08/2026: aviso de sede sustituida se pierde
  cuando hay 2+ alternativas.** En `resolverSede` (`turnos.go`), cuando la
  sede pedida no tiene el servicio y `buscarPorZonaOLocalidad` encuentra
  **una sola** alternativa, el aviso ("Luján no tiene X — te lo cambio a
  otra sede") sale bien. Pero si encuentra **dos o más**, la pregunta
  "¿Cuál preferís: X, Y?" se le devuelve al afiliado directo, sin el aviso
  de por qué se le está preguntando eso — perdiendo el contexto de que la
  sede que pidió no tenía el servicio. Encontrado al armar el material de
  presentación para gerencia (`casos_uso_tecnicos_alma_app.docx`), en un
  caso con una sola alternativa que no ejercitó esta rama. **Fix
  pendiente**: anteponer la nota de sustitución también a la salida de
  "cuál preferís", no solo al caso de una sola alternativa. Agregar test
  que reproduzca 2+ alternativas en la misma zona.
- **Nivel 3 (identidad verificada, línea afiliado) no está implementado.**
  El orquestador detecta cuándo hace falta escalar pero devuelve un mensaje
  de "todavía no implementado" en vez de resolverlo.
- **Nivel 2p (línea prestador) no tiene ningún módulo implementado
  todavía** — Gestión de prestador y Firma Digital siguen en
  `prospecto_prestador.md`, sin spec formal ni código.
- **Persistencia de `ConversationState` sin implementar para producción.**
  El REPL la sostiene en memoria del proceso — en producción vive en
  Redis (TTL 30 min, spec_orquestador.md sección B1).
- **`AgendaFixture` (`internal/modules/turnos/agenda_fixture.go`) lee
  `data/agenda_fixture.json`, no pega contra ningún sistema real.**
  Reemplazar por el cliente real contra la API de agenda sigue pendiente.
  Los datos del fixture son ficticios (ver el archivo, tiene su propia
  nota) — sirven para probar los casos de uso, no para producción.
- **Reintento acotado, teléfono de contacto, y registro de casos sin
  resolver — sin código todavía.** spec_orquestador.md sección 6 ya
  define el patrón completo (3 intentos con opciones + "Otra", 0810 810
  8222 tras el tercero, registro para revisión periódica con su
  trade-off de datos sensibles sin resolver) — nada de esto está
  implementado.
- **Validación cruzada entre `sedes.json` y `especialidades.json` —
  sin resolver a propósito.** Un `id` de servicio mal tipeado en
  `sedes.json` que no exista en `especialidades.json` no se detecta hasta
  que falla en uso.
- **Catálogo de sedes y especialidades hardcodeado.** Decidido: van a
  vivir en `sedes.json` y `especialidades.json`, versionados en el repo,
  cargados una sola vez por `main.go` al arrancar (spec_sedes.md y
  spec_turnos.md, sección 6 de cada una). Hoy `main.go` sigue teniendo un
  array de dos sedes escrito a mano en Go — falta migrar a los archivos.
  Quien consume cada uno: `main.go` los lee al arrancar; el módulo Sedes
  guarda el contenido de `sedes.json`; el módulo Turnos usa
  `especialidades.json` para validar los IDs que recibe; el prompt del
  Clasificador se arma citando `especialidades.json` para que el modelo
  siempre devuelva el mismo identificador. **El Orquestador no consume
  ninguno de los dos** — no conoce el contenido de ningún módulo, solo la
  interface `Module`.
- **Sin validación cruzada entre `sedes.json` y `especialidades.json`.**
  Un error de tipeo en el `id` de un servicio dentro de `sedes.json` (que
  no exista en `especialidades.json`) no se detecta hasta que falla en
  producción. Sin resolver a propósito — queda como próximo paso, no como
  decisión tomada.
- **Reintento acotado y registro de casos sin resolver — sin código
  todavía.** spec_orquestador.md sección 6 ya define el patrón (3
  intentos con opciones concretas + "Otra", teléfono de contacto humano
  tras el tercero, y un registro de los casos que llegan ahí para
  revisión periódica) — nada de esto está implementado. El registro en
  particular tiene un trade-off de datos sensibles sin resolver (mismo
  tipo de chequeo que se necesitó para descartar Supabase) — no
  implementar el guardado hasta que se resuelva esa parte.
- **Verificación de beneficiario (DNI de un integrante del grupo
  familiar) — no es un gap de diseño, es una integración pendiente de
  investigar.** Ya existe en el bot actual de Botmaker; la intención es
  reutilizar esa misma API, no construir una alternativa propia.
- **Terminología "RDS Huawei" pendiente de precisar.** Confirmado que en
  realidad se consume vía una API que refleja el RDS, no acceso directo
  a la base — por seguridad. La corrección de nombre (acá, en las specs,
  y en el informe) queda pendiente para la próxima confección de
  documentos; no es un cambio de arquitectura, solo de precisión.
- **Sedes como dependencia de Turnos es un tipo concreto (`*sedes.Module`),
  no una interface** — a diferencia del Clasificador, que si mañana
  cambiara de proveedor no requiere tocar el resto del código, cambiar
  cómo se resuelven las sedes sí requeriría editar `turnos.go`. Queda
  anotado como inconsistencia de diseño conocida, no resuelta.

## Cómo correrlo

```bash
export ANTHROPIC_API_KEY=tu-key
go run ./cmd/server
```

Levanta un **REPL de terminal**, cargando `data/sedes.json`,
`data/especialidades.json` y `data/agenda_fixture.json`. La sesión demo
arranca con el DNI `20111222` (ya tiene un turno vigente de odontología
en Luján), nivel 2 — así se puede probar el camino completo sin trabarse
en la escalada de nivel 3 (que no está implementada). El programa imprime
al arrancar qué otros DNI de prueba hay disponibles (cupo lleno, beneficiario sin turnos).

Ejemplo de sesión mostrando el caso central de esta ronda — pedir una
especialidad en una sede que no la tiene, y que el sistema ofrezca una
alternativa de la misma zona sin perder de vista qué pasó:
```
> quiero un turno de cardiología en Luján de Cuyo
Luján de Cuyo no tiene cardiologia — te lo cambio a otra sede de la zona.
¿Qué franja preferís? Opciones: 2026-09-03 11:00 | 2026-09-04 09:00

> 2026-09-03 11:00
Turno confirmado: cardiologia el 2026-09-03 a las 11:00 en hiper_libertad.
```

## Cómo correr los tests

```bash
go test ./... -v
```

Ocho tests en `internal/orchestrator/orchestrator_test.go`, con un
clasificador falso (no llama a la API real) para poder probar la lógica
de negocio de forma rápida y repetible:

- `TestSolicitar_YaTieneVigente_NoDuplica`
- `TestSolicitar_LimiteDe3_Bloquea`
- `TestSolicitar_Beneficiario_NoCuentaContraElCupoDelAfiliado`
- `TestSolicitar_SedeSinElServicio_OfreceAlternativaEnLaZona`
- `TestSolicitar_SinServicioEnTodaLaZona_PreguntaAntesDeAmpliar`
- `TestDefensaEnProfundidad_SesionSinNivel_Rechaza`
- `TestDosIntencionesMismoDominio_SeResuelvenAmbas`
- `TestDosIntencionesMismoDominio_UnaPendienteNoBloqueaALaOtra`

## Cómo probar el Clasificador, aislado del resto

Dos formas, las dos necesitan `ANTHROPIC_API_KEY` porque llaman a la API
real — no hay mock del Clasificador, a propósito: lo que se quiere probar
es si interpreta bien el lenguaje, y eso solo lo resuelve el modelo real.

**Test en lote** (`internal/classifier/claude_test.go`) — 17 mensajes
realistas, verificando propiedades estructurales del resultado (no
igualdad exacta, porque el texto libre del modelo varía entre corridas):

```bash
export ANTHROPIC_API_KEY=tu-key
go test ./internal/classifier/... -v
```

Sin la key, el test se saltea limpio (`--- SKIP`), no rompe `go test ./...`.

**REPL aislado** (`cmd/classify-repl`) — para tipear mensajes sueltos y ver
el `ClassificationResult` completo en JSON, sin que el Orquestador ni los
módulos entren en el medio:

```bash
export ANTHROPIC_API_KEY=tu-key
go run ./cmd/classify-repl
```

Comandos especiales en el REPL: `pending` (ver la intención que quedó a
medio resolver), `reset` (limpiarla), `salir`.

**Por qué existe `internal/promptbuilder/`:** el prompt del Clasificador
vivía adentro de `cmd/server/main.go`, privado — ningún otro programa
podía usarlo. Se lo sacó a un paquete compartido para que el REPL de
producción, `classify-repl`, y el test usen exactamente el mismo prompt,
no una copia que se pueda desincronizar.

## Cómo probar el padrón, aislado del resto

Primera pieza del proyecto que consulta un servicio externo por HTTP en
cada llamada, en vez de cargar un archivo local al arrancar — practica el
patrón real de integración (nivel 2 / beneficiario, spec_turnos.md
sección A3a) antes de que exista la integración real.

```bash
go test ./internal/padron/... -v
```

No necesita `ANTHROPIC_API_KEY` — solo acceso a internet, porque consulta
un JSON ficticio alojado en GitHub (`padron_fixture.json`, 3 afiliados de
prueba: dos activos, uno de baja; cualquier otro DNI es "no existe").
**Todavía no está conectado a Turnos ni al Orquestador** — es una pieza
aislada, para probarse sola primero, mismo criterio que se usó con el
Clasificador.

## Estructura

```
cmd/server/main.go                        — wiring, REPL de producción (flujo completo)
cmd/classify-repl/main.go                 — REPL aislado, solo Clasificador
internal/types/                            — contratos compartidos + NivelAfiliadoAlcanza
internal/promptbuilder/                    — arma el prompt + carga especialidades.json (compartido)
internal/padron/                           — cliente HTTP del padrón ficticio (aislado, sin integrar aún)
internal/classifier/                       — interface + implementación Claude API
internal/classifier/claude_test.go         — test del Clasificador contra la API real
internal/modules/                          — interface Module + implementaciones (turnos, sedes)
internal/orchestrator/                     — clasificar → escalar → ejecutar → agregar
internal/orchestrator/orchestrator_test.go — tests de las reglas de negocio
```

## Próximo módulo a spec-ear

Un paquete nuevo bajo `internal/modules/` que implemente `modules.Module`
(tres métodos: `Domain()`, `AccessLevelRequired() string`, `Execute(...)`,
con revalidación de nivel al principio de `Execute` — copiar el patrón de
`turnos.go`/`sedes.go`), más una línea en `main.go` para registrarlo.
