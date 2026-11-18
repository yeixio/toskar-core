package comfyui

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Input names a node field a workflow takes from the chat.
type Input struct {
	Node  string `json:"node"`
	Field string `json:"field"`
}

// Inputs are the fields Toskar fills: what to make, what to avoid, the
// seed, a picture from the chat, and the size.
type Inputs struct {
	Prompt   *Input `json:"prompt,omitempty"`
	Negative *Input `json:"negative,omitempty"`
	Seed     *Input `json:"seed,omitempty"`
	Image    *Input `json:"image,omitempty"`
	Width    *Input `json:"width,omitempty"`
	Height   *Input `json:"height,omitempty"`
}

// Workflow is a ComfyUI workflow in API format that the Admin saved, with
// the inputs Toskar fills.
type Workflow struct {
	// Name is how the model and the Admin call it, such as "upscale".
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Graph       json.RawMessage `json:"graph"`
	Inputs      Inputs          `json:"inputs"`
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// CleanName makes "Remove background" into "remove-background".
func CleanName(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 40 {
		out = strings.TrimRight(out[:40], "-")
	}
	return out
}

type node struct {
	ClassType string         `json:"class_type"`
	Inputs    map[string]any `json:"inputs"`
}

// parseGraph reads a workflow in API format: node IDs to class_type and
// inputs. The editor's own format (with "nodes" and "links") isn't it.
func parseGraph(raw json.RawMessage) (map[string]node, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil || len(probe) == 0 {
		return nil, errors.New("the workflow isn't JSON; export it from ComfyUI with Workflow › Export (API)")
	}
	if _, ok := probe["nodes"]; ok {
		if _, links := probe["links"]; links {
			return nil, errors.New("this is the editor's format; export it from ComfyUI with Workflow › Export (API)")
		}
	}
	graph := map[string]node{}
	for id, v := range probe {
		var n node
		if json.Unmarshal(v, &n) != nil || n.ClassType == "" {
			return nil, fmt.Errorf("node %q has no class_type; export the workflow with Workflow › Export (API)", id)
		}
		graph[id] = n
	}
	return graph, nil
}

// link reads an input that comes from another node: ["4", 0].
func link(v any) (string, bool) {
	l, ok := v.([]any)
	if !ok || len(l) != 2 {
		return "", false
	}
	id, ok := l[0].(string)
	return id, ok
}

// sorted returns node IDs in a stable order, numbers first by value.
func sorted(graph map[string]node) []string {
	ids := make([]string, 0, len(graph))
	for id := range graph {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if len(ids[i]) != len(ids[j]) {
			return len(ids[i]) < len(ids[j])
		}
		return ids[i] < ids[j]
	})
	return ids
}

// textNode follows a sampler's conditioning back to the node that holds
// its text, through nodes such as ControlNet that pass it on.
func textNode(graph map[string]node, id string) *Input {
	for range 8 {
		n, ok := graph[id]
		if !ok {
			return nil
		}
		if _, ok := n.Inputs["text"].(string); ok {
			return &Input{Node: id, Field: "text"}
		}
		next := ""
		for _, key := range []string{"conditioning", "positive", "negative", "conditioning_to", "conditioning_1"} {
			if l, ok := link(n.Inputs[key]); ok {
				next = l
				break
			}
		}
		if next == "" {
			return nil
		}
		id = next
	}
	return nil
}

// Detect finds the usual inputs in a workflow: the text that a sampler's
// positive and negative conditioning come from, its seed, a Load Image
// node, and an empty latent's size.
func Detect(raw json.RawMessage) (Inputs, error) {
	graph, err := parseGraph(raw)
	if err != nil {
		return Inputs{}, err
	}
	var in Inputs
	for _, id := range sorted(graph) {
		n := graph[id]
		if in.Prompt == nil {
			if l, ok := link(n.Inputs["positive"]); ok {
				in.Prompt = textNode(graph, l)
				if l, ok := link(n.Inputs["negative"]); ok {
					in.Negative = textNode(graph, l)
				}
			}
		}
		if in.Seed == nil {
			for _, key := range []string{"seed", "noise_seed"} {
				if _, ok := n.Inputs[key].(float64); ok {
					in.Seed = &Input{Node: id, Field: key}
					break
				}
			}
		}
		if in.Image == nil && n.ClassType == "LoadImage" {
			in.Image = &Input{Node: id, Field: "image"}
		}
		if in.Width == nil && strings.HasPrefix(n.ClassType, "Empty") && strings.Contains(n.ClassType, "Latent") {
			if _, ok := n.Inputs["width"].(float64); ok {
				in.Width = &Input{Node: id, Field: "width"}
			}
			if _, ok := n.Inputs["height"].(float64); ok {
				in.Height = &Input{Node: id, Field: "height"}
			}
		}
	}
	// A workflow without a sampler, such as a FLUX graph with a guider,
	// still has one text box when there's only one.
	if in.Prompt == nil {
		var texts []string
		for _, id := range sorted(graph) {
			if _, ok := graph[id].Inputs["text"].(string); ok {
				texts = append(texts, id)
			}
		}
		if len(texts) == 1 {
			in.Prompt = &Input{Node: texts[0], Field: "text"}
		}
	}
	return in, nil
}

// Check reports a workflow that can't run: a bad name, a graph that isn't
// in API format, or an input naming a node or field it doesn't have.
func (w Workflow) Check() error {
	if !nameRe.MatchString(w.Name) {
		return errors.New("a workflow's name is lowercase letters, numbers, and dashes, such as upscale or remove-background")
	}
	graph, err := parseGraph(w.Graph)
	if err != nil {
		return err
	}
	for label, in := range w.Inputs.each() {
		n, ok := graph[in.Node]
		if !ok {
			return fmt.Errorf("the %s input names node %q, which the workflow doesn't have", label, in.Node)
		}
		if _, ok := n.Inputs[in.Field]; !ok {
			return fmt.Errorf("the %s input names %q on node %q (%s), which it doesn't have", label, in.Field, in.Node, n.ClassType)
		}
	}
	return nil
}

func (in Inputs) each() map[string]Input {
	out := map[string]Input{}
	for label, p := range map[string]*Input{"prompt": in.Prompt, "negative": in.Negative, "seed": in.Seed, "image": in.Image, "width": in.Width, "height": in.Height} {
		if p != nil {
			out[label] = *p
		}
	}
	return out
}

// Values are what a run fills in. Zero values keep the workflow's own.
type Values struct {
	Prompt   string
	Negative string
	// Seed of zero picks a new one, so the same workflow makes something
	// new each time.
	Seed          int64
	Image         string
	Width, Height int
}

// Fill returns the graph with the values set, and the seed it used.
func (w Workflow) Fill(v Values) (map[string]any, int64, error) {
	var graph map[string]any
	if err := json.Unmarshal(w.Graph, &graph); err != nil {
		return nil, 0, err
	}
	set := func(in *Input, value any) error {
		if in == nil {
			return nil
		}
		n, _ := graph[in.Node].(map[string]any)
		inputs, _ := n["inputs"].(map[string]any)
		if inputs == nil {
			return fmt.Errorf("node %q is missing from the workflow", in.Node)
		}
		inputs[in.Field] = value
		return nil
	}
	if v.Prompt != "" {
		if w.Inputs.Prompt == nil {
			return nil, 0, fmt.Errorf("the %s workflow doesn't take a prompt; leave it out", w.Name)
		}
		if err := set(w.Inputs.Prompt, v.Prompt); err != nil {
			return nil, 0, err
		}
	}
	if v.Negative != "" {
		if err := set(w.Inputs.Negative, v.Negative); err != nil {
			return nil, 0, err
		}
	}
	if v.Image != "" {
		if w.Inputs.Image == nil {
			return nil, 0, fmt.Errorf("the %s workflow doesn't take a picture", w.Name)
		}
		if err := set(w.Inputs.Image, v.Image); err != nil {
			return nil, 0, err
		}
	} else if w.Inputs.Image != nil {
		return nil, 0, fmt.Errorf("the %s workflow needs a picture from this chat: give its name as \"file\"", w.Name)
	}
	seed := v.Seed
	if w.Inputs.Seed != nil {
		if seed <= 0 {
			seed = rand.Int64N(1 << 50)
		}
		if err := set(w.Inputs.Seed, seed); err != nil {
			return nil, 0, err
		}
	} else {
		seed = 0
	}
	for _, s := range []struct {
		in *Input
		v  int
	}{{w.Inputs.Width, v.Width}, {w.Inputs.Height, v.Height}} {
		if s.v > 0 {
			// Latent sizes are multiples of 8.
			if err := set(s.in, (s.v+7)/8*8); err != nil {
				return nil, 0, err
			}
		}
	}
	return graph, seed, nil
}

// Store keeps the saved workflows in one file.
type Store struct {
	mu   sync.Mutex
	Path string
}

// List returns the workflows by name.
func (s *Store) List() ([]Workflow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

func (s *Store) read() ([]Workflow, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return []Workflow{}, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Workflow
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(s.Path), err)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

func (s *Store) write(list []Workflow) error {
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// Get returns one workflow by name.
func (s *Store) Get(name string) (Workflow, bool, error) {
	list, err := s.List()
	if err != nil {
		return Workflow{}, false, err
	}
	for _, w := range list {
		if w.Name == name {
			return w, true, nil
		}
	}
	return Workflow{}, false, nil
}

// Save adds a workflow or replaces the one with its name.
func (s *Store) Save(w Workflow) error {
	if err := w.Check(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.read()
	if err != nil {
		return err
	}
	out := list[:0]
	for _, old := range list {
		if old.Name != w.Name {
			out = append(out, old)
		}
	}
	return s.write(append(out, w))
}

// Delete removes a workflow; a name it doesn't have is not an error.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.read()
	if err != nil {
		return err
	}
	out := list[:0]
	for _, old := range list {
		if old.Name != name {
			out = append(out, old)
		}
	}
	return s.write(out)
}
