package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ReadFileTool() *ToolHandler {
	return &ToolHandler{
		Name:        "read_file",
		Description: "Read a file's contents from the working directory. Returns the full text (truncated at 50k bytes).",
		Parameters: map[string]any{
			"path": map[string]any{"type": "string", "description": "Path relative to the working directory"},
		},
		Required:  []string{"path"},
		Sensitive: false,
		Run: func(tc ToolContext, args map[string]any) (string, error) {
			p, _ := args["path"].(string)
			data, err := os.ReadFile(filepath.Join(tc.WorkDir, p))
			if err != nil {
				return "", err
			}
			if len(data) > 50_000 {
				return string(data[:50_000]) + "\n...[truncated]", nil
			}
			return string(data), nil
		},
	}
}

func ListDirTool() *ToolHandler {
	return &ToolHandler{
		Name:        "list_dir",
		Description: "List files and directories at a path, one per line, 'd ' prefix for directories.",
		Parameters: map[string]any{
			"path": map[string]any{"type": "string", "description": "Directory path, default '.'"},
		},
		Sensitive: false,
		Run: func(tc ToolContext, args map[string]any) (string, error) {
			p, _ := args["path"].(string)
			if p == "" {
				p = "."
			}
			entries, err := os.ReadDir(filepath.Join(tc.WorkDir, p))
			if err != nil {
				return "", err
			}
			var b strings.Builder
			for _, e := range entries {
				if e.IsDir() {
					fmt.Fprintf(&b, "d %s\n", e.Name())
				} else {
					fmt.Fprintf(&b, "  %s\n", e.Name())
				}
			}
			return b.String(), nil
		},
	}
}

func WriteFileTool() *ToolHandler {
	return &ToolHandler{
		Name:        "write_file",
		Description: "Create or overwrite a file with the given content. Use generate_edit to produce content for files longer than ~200 lines.",
		Parameters: map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		},
		Required:  []string{"path", "content"},
		Sensitive: true,
		Run: func(tc ToolContext, args map[string]any) (string, error) {
			p, _ := args["path"].(string)
			content, _ := args["content"].(string)
			full := filepath.Join(tc.WorkDir, p)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
				return "", err
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(content), p), nil
		},
	}
}
