package ai

import (
	"encoding/json"
	"net/http"
)

func writeResponse(w http.ResponseWriter, text string) {
	json.NewEncoder(w).Encode(map[string]any{
		"id": "resp-test", "object": "response", "status": "completed",
		"output": []any{map[string]any{"id": "msg-test", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}},
	})
}
