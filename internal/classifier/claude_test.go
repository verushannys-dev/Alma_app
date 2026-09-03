package classifier_test

import (
	"context"
	"os"
	"testing"

	"alma-app/internal/classifier"
	"alma-app/internal/promptbuilder"
	"alma-app/internal/types"
)

// requiereAPIKey salta el test si no hay ANTHROPIC_API_KEY en el entorno —
// así "go test ./..." no rompe en una máquina sin la key configurada, pero
// corre de verdad (contra la API real de Claude, sin mocks) cuando sí está.
func requiereAPIKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY no configurada — saltando test contra la API real")
	}
	return key
}

func construirClasificador(t *testing.T) *classifier.ClaudeClassifier {
	t.Helper()
	especialidades, err := promptbuilder.CargarEspecialidades("../../data/especialidades.json")
	if err != nil {
		t.Fatalf("cargando especialidades.json: %v", err)
	}
	return classifier.NewClaudeClassifier(requiereAPIKey(t), promptbuilder.Construir(especialidades))
}

func sesionBase() types.SessionContext {
	return types.SessionContext{
		IdentitySignalActual: "numero_vinculado_autogestion",
		AccessLevelActual:    "2",
		DNI:                  "20111222",
		RolesConocidos:       []string{"afiliado"},
	}
}

// buscarPorDomain devuelve la primera intención de ese domain, o nil.
func buscarPorDomain(intents []types.Intent, domain string) *types.Intent {
	for i := range intents {
		if intents[i].Domain == domain {
			return &intents[i]
		}
	}
	return nil
}

// TestClasificador corre la tanda completa de mensajes realistas contra la
// API real, verificando propiedades estructurales del resultado — no
// igualdad exacta, porque el texto libre que devuelve el modelo (ej.
// "clarification") varía de una corrida a otra aunque la clasificación de
// fondo sea correcta.
func TestClasificador(t *testing.T) {
	cl := construirClasificador(t)
	ctx := context.Background()
	sesion := sesionBase()

	casos := []struct {
		nombre    string
		mensaje   string
		pending   []types.Intent
		verificar func(t *testing.T, r types.ClassificationResult)
	}{
		{
			nombre:  "solicitar turno simple, todo completo",
			mensaje: "Quiero un turno de odontología en Luján de Cuyo",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				it := buscarPorDomain(r.Intents, "turnos")
				if it == nil {
					t.Fatal("esperaba una intención de dominio turnos")
				}
				if it.Action != "solicitar" {
					t.Errorf("action = %q, esperaba \"solicitar\"", it.Action)
				}
				if it.Params["especialidad"] != "odontologia" {
					t.Errorf("especialidad = %q, esperaba \"odontologia\"", it.Params["especialidad"])
				}
				if it.AccessLevelRequired != "2" {
					t.Errorf("access_level_required = %q, esperaba \"2\"", it.AccessLevelRequired)
				}
			},
		},
		{
			nombre:  "cambiar turno",
			mensaje: "Necesito cambiar mi turno del jueves",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				it := buscarPorDomain(r.Intents, "turnos")
				if it == nil || it.Action != "cambiar" {
					t.Errorf("esperaba action=cambiar, recibí %+v", it)
				}
			},
		},
		{
			nombre:  "anular turno",
			mensaje: "Quiero anular el turno que pedí",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				it := buscarPorDomain(r.Intents, "turnos")
				if it == nil || it.Action != "anular" {
					t.Errorf("esperaba action=anular, recibí %+v", it)
				}
			},
		},
		{
			nombre:  "consultar inasistencia",
			mensaje: "¿Tengo alguna inasistencia registrada?",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				it := buscarPorDomain(r.Intents, "turnos")
				if it == nil || it.Action != "consultar_inasistencia" {
					t.Errorf("esperaba action=consultar_inasistencia, recibí %+v", it)
				}
			},
		},
		{
			nombre:  "especialidad infantil, distingue de la de adultos",
			mensaje: "Necesito un turno de cardiología infantil para mi hija",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				it := buscarPorDomain(r.Intents, "turnos")
				if it == nil {
					t.Fatal("esperaba intención de turnos")
				}
				if it.Params["especialidad"] != "cardiologia_infantil" {
					t.Errorf("especialidad = %q, esperaba \"cardiologia_infantil\"", it.Params["especialidad"])
				}
			},
		},
		{
			nombre:  "especialidad de adultos, no debe confundirse con infantil",
			mensaje: "Quiero sacar un turno de cardiología para mí",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				it := buscarPorDomain(r.Intents, "turnos")
				if it == nil {
					t.Fatal("esperaba intención de turnos")
				}
				if it.Params["especialidad"] != "cardiologia" {
					t.Errorf("especialidad = %q, esperaba \"cardiologia\" (no infantil)", it.Params["especialidad"])
				}
			},
		},
		{
			nombre:  "dos intenciones del mismo dominio, cada una su propio id",
			mensaje: "Quiero un turno de odontología para mí en Luján y cardiología infantil para mi hijo en Maipú",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				var turnos []types.Intent
				for _, it := range r.Intents {
					if it.Domain == "turnos" {
						turnos = append(turnos, it)
					}
				}
				if len(turnos) != 2 {
					t.Fatalf("esperaba 2 intenciones de turnos, recibí %d: %+v", len(turnos), turnos)
				}
				if turnos[0].ID == "" || turnos[1].ID == "" || turnos[0].ID == turnos[1].ID {
					t.Errorf("esperaba dos IDs distintos y no vacíos, recibí %q y %q", turnos[0].ID, turnos[1].ID)
				}
				if len(r.ExecutionOrder) != 2 {
					t.Errorf("execution_order debería tener 2 elementos, tiene %d: %v", len(r.ExecutionOrder), r.ExecutionOrder)
				}
			},
		},
		{
			nombre:  "mensaje incompleto: falta especialidad y sede",
			mensaje: "Quiero un turno",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				it := buscarPorDomain(r.Intents, "turnos")
				if it == nil {
					t.Fatal("esperaba intención de turnos, aunque incompleta")
				}
				if !it.NeedsClarification {
					t.Error("esperaba needs_clarification=true, el mensaje no da especialidad ni sede")
				}
				if it.Clarification == "" {
					t.Error("esperaba una pregunta concreta en clarification")
				}
			},
		},
		{
			nombre:  "búsqueda de sedes por zona (dominio distinto)",
			mensaje: "¿Qué sedes hay en Gran Mendoza?",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				it := buscarPorDomain(r.Intents, "sedes")
				if it == nil {
					t.Fatal("esperaba intención de dominio sedes")
				}
				if it.AccessLevelRequired != "1" {
					t.Errorf("access_level_required = %q, esperaba \"1\" (sedes es público)", it.AccessLevelRequired)
				}
			},
		},
		{
			nombre:  "Caso A — variante de escritura, se resuelve solo sin preguntar",
			mensaje: "Necesito un turno de Odontología en Luján de Cuyo", // mayúscula+tilde (variante) + sede, igual que el caso base que sí pasa — para aislar SOLO la variable de la ortografía
			verificar: func(t *testing.T, r types.ClassificationResult) {
				it := buscarPorDomain(r.Intents, "turnos")
				if it == nil {
					t.Fatal("esperaba intención de turnos")
				}
				if it.Params["especialidad"] != "odontologia" {
					t.Errorf("especialidad = %q, esperaba que reconociera \"odontologia\" pese a la variante de escritura", it.Params["especialidad"])
				}
				if it.NeedsClarification {
					t.Errorf("no debería pedir aclaración por una variante de escritura reconocible — clarification recibida: %q | params completos: %+v", it.Clarification, it.Params)
				}
			},
		},
		{
			nombre:  "Módulo fuera de catálogo — Credencial (módulo 7, sin implementar)",
			mensaje: "Quiero ver mi credencial",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				for _, it := range r.Intents {
					if it.Domain != "turnos" && it.Domain != "sedes" {
						t.Errorf("el Clasificador inventó un domain fuera del catálogo real: %q — esto rompería al Orquestador, que no tiene ningún módulo registrado con ese nombre", it.Domain)
					}
				}
				t.Logf("resultado completo para inspección manual: %+v", r)
			},
		},
		{
			nombre:  "Caso B — especialidad que genuinamente no existe en el catálogo",
			mensaje: "Quiero un turno de traumatología",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				it := buscarPorDomain(r.Intents, "turnos")
				if it == nil {
					t.Fatal("esperaba intención de turnos, aunque no se pueda resolver la especialidad")
				}
				if !it.NeedsClarification {
					t.Error("esperaba needs_clarification=true — traumatología no está en el catálogo")
				}
				if it.Params["especialidad"] == "traumatologia" {
					t.Error("NO debería inventar un id \"traumatologia\" que no existe en especialidades.json")
				}
			},
		},
		{
			nombre:  "turno para un beneficiario del grupo familiar",
			mensaje: "Quiero sacar un turno de odontología para mi hijo",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				it := buscarPorDomain(r.Intents, "turnos")
				if it == nil {
					t.Fatal("esperaba intención de turnos")
				}
				// El Clasificador no conoce el DNI real del hijo, pero debería
				// dejar alguna señal de que el turno NO es para quien escribe
				// (ya sea pidiendo el dato, o marcando el param si lo tuviera).
				if it.Params["especialidad"] != "odontologia" {
					t.Errorf("especialidad = %q, esperaba \"odontologia\"", it.Params["especialidad"])
				}
			},
		},
		{
			nombre:  "slot-filling multi-turno: mensaje previo pendiente se combina",
			mensaje: "odontología",
			pending: []types.Intent{
				{ID: "intent_1", Domain: "turnos", Action: "solicitar", AccessLevelRequired: "2",
					NeedsClarification: true, Params: map[string]string{"sede_id": "lujan_de_cuyo"}},
			},
			verificar: func(t *testing.T, r types.ClassificationResult) {
				it := buscarPorDomain(r.Intents, "turnos")
				if it == nil {
					t.Fatal("esperaba intención de turnos")
				}
				if it.Params["especialidad"] != "odontologia" {
					t.Errorf("especialidad = %q, esperaba \"odontologia\"", it.Params["especialidad"])
				}
				if it.Params["sede_id"] != "lujan_de_cuyo" {
					t.Errorf("esperaba que combinara con el sede_id ya conocido, params = %+v", it.Params)
				}
			},
		},
		{
			nombre:  "cambiar turno, especifica cuál",
			mensaje: "Quiero cambiar el turno número stub-nuevo-1",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				it := buscarPorDomain(r.Intents, "turnos")
				if it == nil || it.Action != "cambiar" {
					t.Errorf("esperaba action=cambiar, recibí %+v", it)
				}
			},
		},
		{
			nombre:  "turno de laboratorio en una localidad puntual",
			mensaje: "Necesito sacar un turno de laboratorio en Maipú",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				it := buscarPorDomain(r.Intents, "turnos")
				if it == nil {
					t.Fatal("esperaba intención de turnos")
				}
				if it.Params["especialidad"] != "laboratorio" {
					t.Errorf("especialidad = %q, esperaba \"laboratorio\"", it.Params["especialidad"])
				}
			},
		},
		{
			nombre:  "detalle de una sede puntual",
			mensaje: "¿Cuál es el horario de la sede de Hiper Libertad?",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				it := buscarPorDomain(r.Intents, "sedes")
				if it == nil || it.Action != "detalle_sede" {
					t.Errorf("esperaba domain=sedes action=detalle_sede, recibí %+v", it)
				}
			},
		},
		{
			nombre:  "mensaje ambiguo, sin ningún dato del pedido",
			mensaje: "Hola, necesito ayuda con algo",
			verificar: func(t *testing.T, r types.ClassificationResult) {
				// No exigimos un domain puntual acá — el criterio es que el
				// Clasificador NO debe inventar una intención confiada
				// (confidence alta) sin base real en el mensaje.
				for _, it := range r.Intents {
					if it.Confidence > 0.6 && !it.NeedsClarification {
						t.Errorf("mensaje sin info concreta no debería producir una intención confiada sin aclarar: %+v", it)
					}
				}
			},
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			resultado, err := cl.ClassifyIntent(ctx, c.mensaje, sesion, c.pending)
			if err != nil {
				t.Fatalf("error clasificando: %v", err)
			}
			if len(resultado.Intents) == 0 {
				t.Fatal("el Clasificador no devolvió ninguna intención")
			}
			c.verificar(t, resultado)
		})
	}
}
