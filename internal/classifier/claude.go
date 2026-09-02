package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"alma-app/internal/types"
)

const claudeAPIURL = "https://api.anthropic.com/v1/messages"

// ClaudeClassifier implementa Classifier contra la API de Anthropic.
// Uso previsto: fase de testing/prototipo. Migrar a Ollama implementando
// esta misma interface (ver classifier.go) cuando esté resuelto en el host de Huawei.
type ClaudeClassifier struct {
	apiKey     string
	model      string
	httpClient *http.Client
	// systemPrompt se arma a partir de los specs de los módulos activos
	// (domain, subdomain, params esperados de cada uno). TODO: generarlo
	// dinámicamente desde el catálogo de módulos en vez de hardcodearlo.
	systemPrompt string
}

// NewClaudeClassifier crea un clasificador contra Claude API.
// apiKey se lee de la variable de entorno ANTHROPIC_API_KEY en main.go —
// nunca hardcodear la key acá.
func NewClaudeClassifier(apiKey, systemPrompt string) *ClaudeClassifier {
	return &ClaudeClassifier{
		apiKey:       apiKey,
		model:        "claude-sonnet-5", // cambiar acá si se quiere otro modelo
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		systemPrompt: systemPrompt,
	}
}

type claudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system"`
	Messages  []claudeMessage `json:"messages"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// ClassifyIntent le pide a Claude que devuelva el JSON de clasificación.
// El system prompt (armado fuera de esta función) es responsable de instruir
// al modelo a devolver *solo* JSON válido con la forma de ClassificationResult.
func (c *ClaudeClassifier) ClassifyIntent(ctx context.Context, mensaje string, sessionCtx types.SessionContext, pending []types.Intent) (types.ClassificationResult, error) {
	var result types.ClassificationResult

	sessionJSON, err := json.Marshal(sessionCtx)
	if err != nil {
		return result, fmt.Errorf("serializando session context: %w", err)
	}

	userContent := fmt.Sprintf("Mensaje del afiliado: %q\nContexto de sesión: %s", mensaje, sessionJSON)

	if len(pending) > 0 {
		pendingJSON, err := json.Marshal(pending)
		if err != nil {
			return result, fmt.Errorf("serializando intenciones pendientes: %w", err)
		}
		userContent += fmt.Sprintf("\nIntenciones pendientes del mensaje anterior (puede haber más de una — combiná el mensaje nuevo con la que corresponda por id, o por contexto si el afiliado no lo aclara, en vez de reclasificar de cero salvo que claramente haya cambiado de tema): %s", pendingJSON)
	}

	reqBody := claudeRequest{
		Model:     c.model,
		MaxTokens: 2000, // 1000 se quedaba corto con 2 intenciones o clarification largo — cortaba el JSON a mitad de camino
		System:    c.systemPrompt,
		Messages: []claudeMessage{
			{Role: "user", Content: userContent},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return result, fmt.Errorf("serializando request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claudeAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return result, fmt.Errorf("creando request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return result, fmt.Errorf("llamando a Claude API: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return result, fmt.Errorf("leyendo respuesta: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("Claude API devolvió status %d: %s", resp.StatusCode, string(respBytes))
	}

	var claudeResp claudeResponse
	if err := json.Unmarshal(respBytes, &claudeResp); err != nil {
		return result, fmt.Errorf("parseando respuesta de Claude: %w", err)
	}

	if len(claudeResp.Content) == 0 {
		return result, fmt.Errorf("respuesta de Claude sin contenido")
	}

	// No asumimos que el primer bloque es el texto — algunos modelos
	// devuelven otros tipos de bloque antes (ej. razonamiento interno).
	// Buscamos el/los bloques de tipo "text" y los concatenamos, en vez
	// de leer Content[0] a ciegas.
	var textoCrudo strings.Builder
	var tiposRecibidos []string
	for _, bloque := range claudeResp.Content {
		tiposRecibidos = append(tiposRecibidos, bloque.Type)
		if bloque.Type == "text" {
			textoCrudo.WriteString(bloque.Text)
		}
	}

	if textoCrudo.Len() == 0 {
		return result, fmt.Errorf("no encontré ningún bloque de tipo \"text\" en la respuesta — tipos de bloque recibidos: %v", tiposRecibidos)
	}

	// El prompt le pide a Claude que devuelva JSON puro, sin backticks de
	// markdown — pero el modelo a veces los pone igual (comportamiento
	// conocido, no exclusivo de este proyecto). Se limpia acá, de forma
	// defensiva, en vez de confiar en que el prompt alcance solo.
	textoLimpio := limpiarBackticksMarkdown(textoCrudo.String())

	if err := json.Unmarshal([]byte(textoLimpio), &result); err != nil {
		return result, fmt.Errorf("el texto de Claude no es JSON válido de ClassificationResult: %w (tipos de bloque: %v, texto recibido: %q)", err, tiposRecibidos, textoLimpio)
	}

	return result, nil
}

// limpiarBackticksMarkdown saca el envoltorio ```json ... ``` o ``` ... ```
// si Claude lo agregó, y recorta espacios en blanco sobrantes. Si el texto
// ya viene sin backticks, lo devuelve tal cual.
func limpiarBackticksMarkdown(texto string) string {
	t := strings.TrimSpace(texto)
	t = strings.TrimPrefix(t, "```json")
	t = strings.TrimPrefix(t, "```")
	t = strings.TrimSuffix(t, "```")
	return strings.TrimSpace(t)
}
