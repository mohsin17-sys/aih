package agent

import (
	"fmt"
	"strings"

	"aiharness/internal/providers"
)

// GenerateEditTool delegates large content generation to the local Qwen editor.
func GenerateEditTool(mlx *providers.MLX) *ToolHandler {
	return &ToolHandler{
		Name: "generate_edit",
		Description: "Generate a complete file body using the local editor model (Qwen on MLX). Use for files longer than ~200 lines or when a large edit is needed. Provide a precise specification referencing real APIs/signatures. Returns the generated content; the caller must then save it with write_file.",
		Parameters: map[string]any{
			"file_path": map[string]any{"type": "string", "description": "Path of the file being generated"},
			"spec":      map[string]any{"type": "string", "description": "Detailed specification of the file's contents"},
		},
		Required:  []string{"file_path", "spec"},
		Sensitive: false,
		Run: func(tc ToolContext, args map[string]any) (string, error) {
			path, _ := args["file_path"].(string)
			spec, _ := args["spec"].(string)

			system := fmt.Sprintf(`You are a code editor. Generate the COMPLETE contents of %s.
Output ONLY the file contents — no markdown fences, no commentary, no explanations.
Follow the specification exactly. Use only APIs that appear in the specification.`, path)
			content, err := mlx.Complete(system, spec)
			if err != nil {
				return "", err
			}
			content = strings.TrimSpace(content)
			if strings.HasPrefix(content, "```") {
				if i := strings.Index(content, "\n"); i > 0 {
					content = content[i+1:]
				}
				if j := strings.LastIndex(content, "```"); j >= 0 {
					content = content[:j]
				}
			}
			return strings.TrimSpace(content), nil
		},
	}
}
