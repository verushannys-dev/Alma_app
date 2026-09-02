// Package types define los contratos compartidos entre el orquestador,
// el clasificador de intents y los módulos de servicio.
//
// Estos tipos son la traducción directa de los JSON de los specs
// (spec_orquestador.md, spec_turnos.md, spec_sedes.md).
// Cualquier cambio acá impacta a todos los módulos — versionar con cuidado.
package types

// SessionContext representa el estado de la sesión del afiliado.
//
// AccessLevelActual es un string, no un número — ver spec_orquestador.md
// sección 4a. La línea afiliado usa "1"/"2"/"3" (con orden real entre
// ellos: 3 exige más que 2). La línea prestador usa "1"/"2p", una escala
// aparte, no comparable numéricamente con la de afiliado.
//
// TODO(dos líneas): este campo hoy solo sigue la línea afiliado — no hay
// todavía ningún módulo de la línea prestador implementado en código
// (Gestión de prestador y Firma Digital siguen en prospecto_prestador.md,
// sin spec formal). Cuando se implemente el primero, este struct va a
// necesitar un campo separado para el nivel de la línea prestador, porque
// una misma persona puede tener las dos sesiones vigentes a la vez.
//
// TODO(nivel-3): la validación de identidad verificada NO está
// implementada todavía. AccessLevelActual hoy solo puede llegar a "2"
// (número vinculado, sin segundo factor). Ver spec_orquestador.md
// sección 8.
//
// TODO(persistencia): en producción esto vive en Redis, TTL 30 min
// (spec_orquestador.md sección 2). Hoy el REPL lo sostiene en memoria del
// proceso mientras dura la sesión de terminal.
type SessionContext struct {
	IdentitySignalActual string `json:"identity_signal_actual"` // "numero_vinculado_autogestion" | "token" | "ninguna"
	AccessLevelActual    string `json:"access_level_actual"`    // "1" | "2" | "3" (línea afiliado, ver comentario arriba)
	// DNI del afiliado titular de esta sesión, ya vinculado a nivel 2.
	// Este campo solo TRANSPORTA el dato — la verificación en sí (que ese
	// DNI existe en el padrón) es un mecanismo externo, ya resuelto en el
	// bot actual, pendiente de investigar qué API reutilizar
	// (spec_orquestador.md sección 8). No confundir con verificar acá.
	DNI            string   `json:"dni"`
	TokenExpiresAt *string  `json:"token_expires_at"` // ISO datetime, nil si no aplica
	RolesConocidos []string `json:"roles_conocidos"`
}

// Intent es una intención detectada por el clasificador dentro de un mensaje.
// El orquestador siempre trabaja con una lista, incluso si hay una sola.
type Intent struct {
	// ID identifica esta intención dentro de la clasificación, asignado
	// por el clasificador (ej. "intent_1", "intent_2"). Es lo que permite
	// tener dos intenciones del mismo Domain en el mismo mensaje sin que
	// se pisen — spec_orquestador.md sección B3.
	ID                  string            `json:"id"`
	Domain              string            `json:"domain"`
	Subdomain           *string           `json:"subdomain"` // nil si el módulo no tiene subdominios (ej. Sedes)
	Action              string            `json:"action"`
	Confidence          float64           `json:"confidence"`
	AccessLevelRequired string            `json:"access_level_required"` // string — ver SessionContext.AccessLevelActual
	NeedsClarification  bool              `json:"needs_clarification"`
	Clarification       string            `json:"clarification,omitempty"`
	Params              map[string]string `json:"params,omitempty"`
}

// Escalation describe el paso de escalada de acceso necesario, si lo hay.
type Escalation struct {
	TargetLevel string `json:"target_level"` // string, mismo criterio que arriba
	Method      string `json:"method"`       // ej. "identity_verification" — hoy no implementado, ver TODO(nivel-3)
}

// ClassificationResult es la salida completa del clasificador,
// contrato de la sección 4 de spec_orquestador.md.
type ClassificationResult struct {
	Intents          []Intent    `json:"intents"`
	ExecutionOrder   []string    `json:"execution_order"`
	EscalationNeeded *Escalation `json:"escalation_needed,omitempty"`
	RolesApplicable  []string    `json:"roles_applicable"`
	IdentitySignal   string      `json:"identity_signal"`
}

// ModuleInput es lo que el orquestador le pasa a cada módulo al invocarlo.
// Sigue el contrato definido individualmente en cada spec de módulo
// (ej. spec_turnos.md sección 4, spec_sedes.md sección 4).
type ModuleInput struct {
	Domain              string  `json:"domain"`
	Subdomain           *string `json:"subdomain"`
	Action              string  `json:"action"`
	Confidence          float64 `json:"confidence"`
	AccessLevelRequired string  `json:"access_level_required"` // lo que dijo el CLASIFICADOR — referencia, no fuente de verdad
	// SessionAccessLevelActual es el nivel real de la sesión en este
	// momento. Existe específicamente para que el módulo pueda comparar
	// su propio AccessLevelRequired() contra esto — nunca contra
	// AccessLevelRequired de arriba, que es una opinión del clasificador
	// (un LLM), no un hecho. Ver types.NivelAfiliadoAlcanza.
	SessionAccessLevelActual string `json:"session_access_level_actual"`
	// SessionDNI es el DNI del afiliado titular de la sesión (ya
	// verificado a nivel 2 — ver types.SessionContext.DNI). Los módulos
	// lo usan como default cuando params["beneficiario_dni"] viene vacío
	// (spec_turnos.md sección 2a: si no se especifica, se asume que es
	// para el afiliado que escribe).
	SessionDNI        string            `json:"session_dni"`
	IdentitySignal    string            `json:"identity_signal"`
	RolesApplicable   []string          `json:"roles_applicable"`
	SessionContextRef string            `json:"session_context_ref"` // TODO(persistencia): ver nota en SessionContext
	Params            map[string]string `json:"params"`
}

// ModuleOutput es lo que cada módulo le devuelve al orquestador.
// "Data" lleva el payload específico del módulo (ej. lista de sedes,
// datos de un turno) — cada módulo define su propia forma dentro de Data,
// el orquestador no necesita conocerla para agregarla al mensaje final.
type ModuleOutput struct {
	Status        string         `json:"status"` // "ok" | "error" | "needs_more_info"
	MessageToUser string         `json:"message_to_user"`
	MissingParams []string       `json:"missing_params,omitempty"`
	Data          map[string]any `json:"data,omitempty"`
}

// nivelAfiliadoOrden da el orden real que sí existe DENTRO de la línea
// afiliado (1 < 2 < 3). No usar para comparar contra la línea prestador
// ("2p") — esa es una escala aparte, sin orden compartido con esta.
var nivelAfiliadoOrden = map[string]int{"1": 1, "2": 2, "3": 3}

// NivelAfiliadoAlcanza compara dos niveles de la línea afiliado.
// Tanto el Orquestador como cada módulo la llaman por su cuenta, cada uno
// con los datos que tiene — es lo que hace real la defensa en profundidad
// (spec_orquestador.md sección 9): un módulo nunca debería confiar en que
// "el Orquestador ya lo validó", vuelve a comparar él mismo.
func NivelAfiliadoAlcanza(actual, requerido string) bool {
	return nivelAfiliadoOrden[actual] >= nivelAfiliadoOrden[requerido]
}
