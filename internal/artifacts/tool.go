package artifacts

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// maxCreateBytes caps a file the model writes in one call.
const maxCreateBytes = 2 << 20

// creatable are the file types the assistant may produce.
var creatable = map[string]bool{
	".md": true, ".txt": true, ".html": true, ".json": true, ".csv": true, ".tsv": true, ".xlsx": true,
	".docx": true, ".pdf": true,
	".py": true, ".js": true, ".ts": true, ".go": true, ".rs": true, ".java": true, ".c": true, ".cpp": true,
	".rb": true, ".php": true, ".sh": true, ".sql": true, ".yaml": true, ".yml": true, ".toml": true,
	".xml": true, ".css": true, ".swift": true, ".kt": true,
}

type convKey struct{}

// WithConversation tells files.create which chat a file belongs to.
func WithConversation(ctx context.Context, conversationID string) context.Context {
	return context.WithValue(ctx, convKey{}, conversationID)
}

func conversationFrom(ctx context.Context) string {
	id, _ := ctx.Value(convKey{}).(string)
	return id
}

// CreateTool is files.create: the assistant makes a file the user can
// download. The file goes into this store only; nothing else on the
// computer changes.
type CreateTool struct {
	Store *Store
}

func (t *CreateTool) ID() string          { return "files.create" }
func (t *CreateTool) DisplayName() string { return "Create File" }
func (t *CreateTool) Description() string {
	return "Create a file the user can download"
}

// formats are the extensions the capability names ask for (Gungnir §21–22):
// document.create, pdf.create, and spreadsheet.create reach this tool with
// format set.
var formats = map[string]string{"docx": ".docx", "pdf": ".pdf", "xlsx": ".xlsx"}

// Execute saves the file. A .xlsx name takes CSV text, where "## Sheet: Name"
// lines start more sheets and cells starting with = are formulas. .docx and
// .pdf names take Markdown. A name without a known extension is saved as
// Markdown.
func (t *CreateTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	name, _ := args["name"].(string)
	content, _ := args["content"].(string)
	name = CleanName(name)
	if f, _ := args["format"].(string); formats[f] != "" && !strings.EqualFold(filepath.Ext(name), formats[f]) {
		name = strings.TrimSuffix(name, filepath.Ext(name)) + formats[f]
	}
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("content required")
	}
	if len(content) > maxCreateBytes {
		return nil, fmt.Errorf("the file is larger than %d MB", maxCreateBytes>>20)
	}
	ext := strings.ToLower(filepath.Ext(name))
	if !creatable[ext] {
		name += ".md"
		ext = ".md"
	}
	data := []byte(content)
	var err error
	switch ext {
	case ".xlsx":
		data, err = CSVToXLSX(strings.TrimSuffix(name, filepath.Ext(name)), content)
	case ".docx":
		data, err = MarkdownToDOCX(content)
	case ".pdf":
		data, err = MarkdownToPDF(content)
	}
	if err != nil {
		return nil, err
	}
	a, err := t.Store.Save(ctx, Input{
		ConversationID: conversationFrom(ctx),
		Name:           name,
		Producer:       ProducerAssistant,
		Data:           data,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":         a.ID,
		"name":       a.Name,
		"kind":       a.Kind,
		"mime_type":  a.MimeType,
		"size_bytes": a.Size,
		"note":       "The file is attached to your answer as a download. Tell the user what it contains; do not repeat its contents.",
	}, nil
}
