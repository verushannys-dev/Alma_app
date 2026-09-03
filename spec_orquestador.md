# Spec: Orquestador (ALMA APP)

> Estado: v1.5 — 28/08/2026. Cambio respecto a v1.4: se agrega
> `domain: "no_disponible"` como tercer valor válido del contrato
> (sección B3) — evita que el Clasificador invente un `domain` con el
> nombre de una intención real pero sin módulo implementado (encontrado
> probando "quiero ver mi credencial" contra la API real). Implementado
> en el prompt y testeado; el lado del Orquestador que lo consume queda
> pendiente para la etapa de integración. A partir de esta presentación,
> todo cambio que toque arquitectura actualiza versión acá y en el
> informe de gestión de producto a la vez.

Consume las specs de los módulos (ver plantilla y spec de Turnos). Responsable de interpretar el pedido del afiliado, resolver qué módulo(s) invocar y en qué orden, y devolver una respuesta única aunque haya más de una intención en juego.

---

# A. Reglas de negocio

Lo que se decidió y por qué — independiente de cómo termine representado en código.

## A1. Responsabilidad del orquestador
- Recibir el mensaje del afiliado (texto libre o botón) más el contexto de sesión disponible, incluida cualquier intención que haya quedado pendiente del mensaje anterior.
- Clasificar una o más intenciones presentes en el mensaje, combinando el mensaje nuevo con la intención pendiente si la hay. Cada intención debe poder identificarse individualmente, incluso si dos comparten el mismo dominio.
- Determinar el nivel de acceso que cada intención requiere.
- Resolver el orden de ejecución por intención puntual, no por dominio, según dependencias entre ellas.
- Escalar validación de identidad cuando falte, una sola vez por nivel aunque varias intenciones lo requieran.
- Invocar cada módulo con el contrato definido y combinar las respuestas en un solo mensaje al afiliado.
- **No confiar ciegamente en el nivel de acceso que reporta el clasificador** — cada módulo revalida el suyo de forma independiente antes de ejecutar (ver B4).

## A2. Proceso — secuencia de decisiones

1. **Clasificar intención(es)**, combinando el mensaje con cualquier intención pendiente de la sesión.
2. **Resolver nivel de acceso por intención**, consultando la spec de cada módulo candidato (es una propiedad fija del módulo, expresada como string — A3).
3. **Armar la cola de ejecución** ordenando por dependencia, no por orden de mención: las intenciones que son precondición de otras (ej. escalar acceso) van primero. Dos intenciones del mismo dominio (ej. dos turnos) se ejecutan ambas, cada una con sus propios parámetros.
4. **Comparar contra el nivel actual de la sesión** por igualdad exacta de string, no por rango numérico. Si alcanza para todas las intenciones de la tanda, se saltea el paso de escalamiento. Si no alcanza para alguna, se dispara una única escalada al nivel más alto que haga falta — no una por cada intención que lo requiera.
5. **Invocar el módulo correspondiente a cada intención** de la cola. El módulo vuelve a validar su propio nivel antes de ejecutar (B4) — el orquestador no es la única barrera.
6. **Agregar las respuestas** de todas las intenciones resueltas en un solo mensaje ordenado para el afiliado.
7. **Actualizar el estado de la conversación**: si alguna intención quedó a mitad de resolver, se guarda para el próximo mensaje.

## A3. Niveles de acceso — dos líneas no comparables

No hay una escala jerárquica única. Cada línea define sus propios valores:

| Línea | Valores | Qué garantiza cada uno |
|---|---|---|
| Afiliado | "1" | Público, nada |
| Afiliado | "2" | Existe en el padrón (número vinculado a autogestión) — o, si el turno es para un beneficiario del grupo familiar, el DNI de ese beneficiario existe en el padrón como integrante del mismo grupo (ver spec_turnos.md sección A3) |
| Afiliado | "3" | Identidad verificada — **mecanismo sin definir**, gap prioritario |
| Prestador | "1" | Público, nada |
| Prestador | "2p" | CUIT existe en ARCA — chequeo de negocio, no prueba de identidad |

"2p" no es "más" ni "menos" que "2" — son verificaciones de naturaleza distinta. La comparación es por igualdad exacta de string contra lo que el módulo declara, nunca por orden. Los niveles se definen módulo a módulo, no por una regla general.

## A4. Manejo de multi-intención — reglas
- Un mensaje puede resolver en 1 a N intenciones; el contrato siempre es una lista, incluso cuando hay una sola. Dos o más intenciones pueden compartir el mismo dominio.
- Si dos o más intenciones comparten el mismo nivel de acceso requerido (mismo string, misma línea), se resuelve **una sola escalada** para todas.
- Si el afiliado pide explícitamente algo que el orquestador iba a hacer de todos modos como precondición, no se duplica el paso — se reconoce como la misma necesidad.
- Si dos intenciones son independientes (no comparten dependencia), el orden de ejecución no importa para el resultado, solo para el orden en que se le presentan al afiliado.
- Dos intenciones del mismo dominio no se bloquean entre sí: si una queda a mitad de resolver y la otra se resuelve directo, la resuelta se confirma igual — no se espera a que las dos estén listas para responder.

## A5. Manejo de ambigüedad

**Disparador:** no es un umbral fijo de confianza, es la distancia entre el candidato top y el segundo.

**Formato de la pregunta:** siempre opciones concretas con botones, nunca una pregunta abierta.

**Ambigüedad de rol:** si la persona tiene más de un rol activo (afiliado y prestador, por ejemplo), un mismo texto puede ser ambiguo por rol y no por módulo. La pregunta de desambiguación en ese caso es sobre el rol, no sobre el dominio.

**Alcance:** la ambigüedad en una intención puntual no bloquea al resto. Una sola pregunta por intención ambigua, nunca se repite todo el menú.

**Reintento acotado — patrón concreto** (definido pensando en el perfil de usuario del proyecto, con poca práctica de tecnología): hasta **3 intentos de reencauce** antes de dar por perdido el mensaje. Cada intento ofrece **3 opciones concretas, calculadas según el contexto de la conversación, más una cuarta opción fija "Otra"**:

```
Intento 1: [opción A] [opción B] [opción C] [Otra]
  → si "Otra": Intento 2, con 3 opciones reformuladas + [Otra]
    → si "Otra" de nuevo: Intento 3, última reformulación + [Otra]
      → si "Otra" de nuevo: se agota el reencauce, ver abajo
```

No es una pregunta abierta repetida tres veces — cada intento arriesga candidatas concretas distintas.

**Al agotarse el reencauce (tercer "Otra" sin resolver):**
1. Se ofrece el menú de intenciones de primer nivel.
2. Se agrega un canal de contacto humano, **fuera de ALMA APP** — no es una derivación gestionada por el sistema (sin cola, sin estado de "conversación transferida"), es información de contacto que ya existe independientemente del bot: *"o comunicate al 0810 810 8222 si preferís hablar con alguien."* Redactado sin asumir un canal particular — el canal final todavía no está definido (ver informe de gestión de producto, sección 11).

**Registro de casos sin resolver:** cuando se llega a este punto, se genera un registro (mensaje original, opciones ofrecidas en cada intento, por qué ninguna resolvió) para revisión humana periódica — detectar si hace falta ajustar el prompt del clasificador, o si el pedido corresponde a un módulo que todavía no existe en el catálogo. No es aprendizaje automático — es insumo para revisión manual, iterativa.

> ⚠️ **Trade-off sin resolver, marcado a propósito:** este registro puede contener datos sensibles (ej. un DNI de beneficiario, una especialidad médica puntual). Antes de definir dónde se guarda y por cuánto tiempo, hace falta el mismo tipo de chequeo de política de datos que ya se aplicó para descartar Supabase (informe de gestión de producto, sección 4). No implementar el guardado hasta que esto se resuelva.

---

# B. Contrato técnico

Cómo se representa todo lo anterior en el intercambio de datos.

## B1. Input que recibe
```json
{
  "mensaje": "string (texto libre o payload de botón)",
  "session_context": {
    "identity_signal_actual": "numero_vinculado_autogestion | token | ninguna",
    "access_level_actual": "1 | 2 | 3 | 2p",
    "dni": "string — DNI del afiliado titular de la sesión, ya vinculado a nivel 2",
    "token_expires_at": "ISO datetime | null",
    "roles_conocidos": ["afiliado"]
  }
}
```
`dni` — **implementado** (`types.SessionContext.DNI`). Solo transporta el dato ya verificado por un mecanismo externo — no lo verifica el orquestador (ver spec_turnos.md sección A3).

Persistencia de `session_context`: Redis, VM interna de OSEP, TTL 30 min. Se descartó Supabase — ver informe de gestión de producto, sección 4. **El código todavía sostiene esto en memoria del proceso (REPL), no en Redis** — ver sección D.

## B2. Estado de conversación (slot-filling multi-turno)

Lo que hace falta sostener *entre* mensajes de una misma conversación:

```json
{
  "session": { "...": "el session_context de B1" },
  "pending": [
    {
      "id": "intent_1",
      "domain": "turnos", "subdomain": "especialista", "action": "solicitar",
      "params": { "especialidad": "odontologia", "sede_id": "luján" }
    },
    {
      "id": "intent_2",
      "domain": "turnos", "subdomain": "especialista", "action": "solicitar",
      "params": { "especialidad": "cardiologia_infantil", "beneficiario_dni": "..." }
    }
  ]
}
```

`pending` como **lista** (no una sola intención) es necesario porque dos turnos del mismo mensaje pueden dejar más de una intención abierta a la vez. **Implementado** — `ConversationState.Pending` es `[]types.Intent`.

## B3. Output (contrato de clasificación)

```json
{
  "intents": [
    {
      "id": "intent_1",
      "domain": "turnos", "subdomain": "especialista", "action": "solicitar",
      "confidence": 0.95, "access_level_required": "2",
      "needs_clarification": false, "clarification": "",
      "params": {"especialidad": "odontologia", "sede_id": "luján"}
    }
  ],
  "execution_order": ["intent_1"],
  "escalation_needed": null,
  "roles_applicable": ["afiliado"],
  "identity_signal": "numero_vinculado_autogestion"
}
```

- **`id`**: identificador único por intención, asignado por el clasificador. Permite tener dos intenciones del mismo `domain` en el mismo mensaje sin que se pisen. **Implementado** (`types.Intent.ID`).
- **`execution_order`**: lista de `id`, no de `domain`. **Implementado** (`orchestrator.go`, `buscarIntentPorID`). Testeado (`TestDosIntencionesMismoDominio_SeResuelvenAmbas`, `TestDosIntencionesMismoDominio_UnaPendienteNoBloqueaALaOtra`).
- **`access_level_required`**: string — **implementado**.
- **`params`**: objeto libre, específico de cada módulo. Se combina con el `params` del `pending` correspondiente si había una intención abierta.
- **`clarification`**: pregunta concreta, con opciones cuando sea posible (A5).
- **`escalation_needed.method`**: `"identity_verification"` para línea afiliado (placeholder, mecanismo sin definir). Para línea prestador, ya identificado: validar CUIT contra ARCA — falta la integración concreta.
- **`domain: "no_disponible"`** — tercer valor válido de `domain`, además de `"turnos"` y `"sedes"`. Se usa cuando el Clasificador entiende con claridad la intención del afiliado, pero corresponde a un módulo del catálogo que todavía no existe en código (ej. "quiero ver mi credencial" → módulo 7, sin spec ni implementación). El Clasificador **nunca** debe inventar un `domain` con el nombre de la intención real (eso rompería al Orquestador, que no tendría ningún módulo registrado con ese nombre) — en su lugar devuelve este valor fijo, con `needs_clarification: false` y `clarification: "Tu pedido aún no puede ser gestionado por Alma. ¿Puedo guiarte con algo más?"`. **Implementado en el prompt** (`internal/promptbuilder/promptbuilder.go`) y **testeado contra la API real** (`internal/classifier/claude_test.go`). **Pendiente**: el lado del Orquestador — hoy `orchestrator.go` intentaría buscar un módulo registrado como `"no_disponible"` y fallaría, porque ninguno existe con ese nombre. Falta el caso especial que, al ver este `domain`, devuelva `Clarification` directo como mensaje final sin pasar por `o.modulos[...]`. Se implementa en la etapa de integración con el Orquestador, no en esta.

## B4. Defensa en profundidad — implementación

El nivel de acceso que reporta el clasificador es una salida de un LLM — puede equivocarse o ser manipulado (prompt injection). Por eso cada módulo revalida el suyo de forma independiente, sin confiar en lo que dijo el clasificador. **Implementado**: `ModuleInput.SessionAccessLevelActual` transporta el nivel real de la sesión (no lo que dijo el clasificador), y cada módulo lo compara contra su propio `AccessLevelRequired()` al entrar a `Execute` (`turnos.go`, `sedes.go`). Testeado (`TestDefensaEnProfundidad_SesionSinNivel_Rechaza`).

---

# C. Casos de prueba

| # | Entrada | `intents` | `execution_order` | Nota | Estado en código |
|---|---|---|---|---|---|
| 1 | "Quiero sacar un turno de laboratorio" | 1 (turnos) | [intent_1] | Sin dependencias | ✅ implementado |
| 2 | "Quiero el token para el médico y ver un resultado" | 2 (token, resultados) | [intent_1, intent_2] | Ambas requieren "3"; una sola escalada | ⚠️ módulos de token/resultados sin implementar todavía |
| 3 | "Quiero ver mi resultado" (ya tiene "3" vigente) | 1 (resultados) | [intent_1] | No se vuelve a escalar | ⚠️ ídem |
| 4 | "Quiero sacar un turno y ver mi cartilla" | 2 (turnos, cartilla) | orden indistinto | Independientes, distinto nivel cada una | ⚠️ módulo cartilla sin implementar |
| 5 | "Quiero un turno" (sin especialidad ni sede) | 1 (turnos), ambiguo | [intent_1] | Pendiente se guarda con lo que sí se sabe | ✅ implementado |
| 6 | "odontología" (mensaje siguiente al 5) | 1 (turnos), combinado | [intent_1] | Completa `especialidad`, sigue faltando sede | ✅ implementado |
| 7 | "Necesito un turno" (rol afiliado y prestador a la vez) | 1 (turnos), ambiguo | [intent_1] | Ambigüedad de rol | ⚠️ sin implementar |
| 8 | "Quiero un turno de odontología para mí en Luján y cardiología infantil para mi hijo en Maipú" | 2, mismo dominio, `id` distinto | [intent_1, intent_2] | El caso que destapó el gap de `execution_order` | ✅ implementado y testeado (`TestDosIntencionesMismoDominio_SeResuelvenAmbas`) |

---

# D. Pendientes / gaps abiertos

- **Mecanismo de verificación de identidad, nivel 3 (línea afiliado).** Sin definir — gap prioritario. Interface declarada, sin implementación:
  ```go
  type IdentityVerifier interface {
      Verificar(ctx context.Context, señal string, sessionCtx SessionContext) (nuevoNivel string, err error)
  }
  ```
- **Integración con ARCA, nivel 2p (línea prestador).** Mecanismo identificado, falta la API/forma de consulta concreta.
- **Techo de la línea prestador.** Hoy 2p es el nivel más alto que existe para esa línea.
- **Verificación de beneficiario por DNI contra el padrón** — ya resuelta en el bot actual, pendiente investigar qué API reutilizar (spec_turnos.md sección A3).
- **Reintento acotado (3 intentos + Otra), teléfono de contacto, y registro de casos sin resolver (A5) — sin código todavía.**
- **Persistencia en Redis — sin implementar.** El REPL sostiene todo en memoria del proceso.
- **Timeout de la cola de ejecución si un módulo no responde** — sin definir.
- **Elección final de canal** (WhatsApp nativo / Flows / PWA / híbrido) — ver informe de gestión de producto, sección 11.
- **Validación cruzada `sedes.json` ↔ `especialidades.json`** — sin resolver a propósito (ver spec_sedes.md sección D).
