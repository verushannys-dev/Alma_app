// Comando classify-repl: prueba SOLO el Clasificador, aislado del
// Orquestador y de los módulos — para inspeccionar exactamente qué le
// llegaría al Orquestador (el types.ClassificationResult completo, en
// JSON) antes de que exista un Orquestador corriendo de verdad, o para
// depurar el prompt sin la ruidosidad del resto del sistema.
//
// No modifica ni depende de cmd/server/main.go — es un punto de entrada
// separado, que comparte con la producción solo el paquete
// internal/promptbuilder (mismo prompt exacto, no una copia).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"alma-app/internal/classifier"
	"alma-app/internal/promptbuilder"
	"alma-app/internal/types"
)

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("falta la variable de entorno ANTHROPIC_API_KEY")
	}

	especialidades, err := promptbuilder.CargarEspecialidades("data/especialidades.json")
	if err != nil {
		log.Fatalf("cargando especialidades.json: %v", err)
	}

	cl := classifier.NewClaudeClassifier(apiKey, promptbuilder.Construir(especialidades))

	fmt.Println("classify-repl — prueba SOLO el Clasificador (sin Orquestador, sin módulos).")
	fmt.Println("Escribí un mensaje y Enter. \"salir\" para terminar, \"pending\" para ver/limpiar la intención pendiente.")
	fmt.Println()

	sesion := types.SessionContext{
		IdentitySignalActual: "numero_vinculado_autogestion",
		AccessLevelActual:    "2",
		DNI:                  "20111222",
		RolesConocidos:       []string{"afiliado"},
	}
	var pending []types.Intent

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		mensaje := strings.TrimSpace(scanner.Text())
		if mensaje == "" {
			continue
		}
		if mensaje == "salir" || mensaje == "exit" {
			break
		}
		if mensaje == "pending" {
			if len(pending) == 0 {
				fmt.Println("(no hay ninguna intención pendiente)")
			} else {
				b, _ := json.MarshalIndent(pending, "", "  ")
				fmt.Println(string(b))
			}
			fmt.Println()
			continue
		}
		if mensaje == "reset" {
			pending = nil
			fmt.Println("(pending limpiado)")
			fmt.Println()
			continue
		}

		resultado, err := cl.ClassifyIntent(context.Background(), mensaje, sesion, pending)
		if err != nil {
			fmt.Printf("[error] %v\n", err)
			continue
		}

		b, err := json.MarshalIndent(resultado, "", "  ")
		if err != nil {
			fmt.Printf("[error serializando] %v\n", err)
			continue
		}
		fmt.Println(string(b))

		// Igual que haría el Orquestador: lo que queda con needs_clarification
		// se guarda como pending para el próximo mensaje.
		var nuevoPending []types.Intent
		for _, it := range resultado.Intents {
			if it.NeedsClarification {
				nuevoPending = append(nuevoPending, it)
			}
		}
		pending = nuevoPending
		if len(pending) > 0 {
			fmt.Printf("  (quedó pendiente: %d intención/es — escribí \"pending\" para verla, \"reset\" para limpiarla)\n", len(pending))
		}
		fmt.Println()
	}
}
