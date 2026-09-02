// Package classifier define la interface de clasificación de intents.
//
// El orquestador depende solo de esta interface, nunca de una implementación
// concreta. Hoy corre contra Claude API (ver claude.go); la idea es migrar
// a Ollama sobre el host de Huawei más adelante sin tocar el orquestador —
// solo agregar una implementación nueva de esta interface.
package classifier

import (
	"context"

	"alma-app/internal/types"
)

// Classifier clasifica el mensaje de un afiliado en una o más intenciones,
// devolviendo el contrato definido en spec_orquestador.md sección B3.
//
// pending son las intenciones que quedaron a mitad de resolver en el
// mensaje anterior (needs_more_info), o vacío si no hay ninguna — puede
// ser más de una (ej. dos turnos del mismo mensaje, cada uno esperando un
// dato distinto). El clasificador debe combinar el mensaje nuevo con la
// que corresponda por ID (o por contexto si el afiliado no lo aclara) en
// vez de reclasificar todo de cero — es lo que permite el slot-filling
// multi-turno.
type Classifier interface {
	ClassifyIntent(ctx context.Context, mensaje string, sessionCtx types.SessionContext, pending []types.Intent) (types.ClassificationResult, error)
}
