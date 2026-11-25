package app

import (
	"errors"
	"net/http"
)

// ollamaEmbed embeds texts for Ollama's /api/embed (#1356) with the
// embedding model knowledge uses, and names it.
func (a *App) ollamaEmbed(r *http.Request, texts []string) ([][]float32, string, error) {
	if a.knowledgeModels == nil {
		return nil, "", errors.New("embeddings aren't available on this computer")
	}
	e, err := a.knowledgeModels.Embedder(r.Context())
	if err != nil {
		return nil, "", err
	}
	if e == nil {
		return nil, "", errors.New("no embedding model is installed: set one up in Settings › Features, under Search knowledge by meaning")
	}
	vectors, err := e.EmbedDocuments(r.Context(), texts)
	return vectors, e.ModelID(), err
}
