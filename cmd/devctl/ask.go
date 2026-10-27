package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	toskar "github.com/yeixio/toskar-sdks/go"

	"github.com/yeixio/toskar-core/internal/askdata"
	"github.com/yeixio/toskar-core/internal/config"
)

const askUsage = `usage: toskarctl ask [flags] [prompt]
  Sends one prompt and prints the answer, as a step in a script or CI job (#455).
  Text piped in, or --file, is sent as data to work on, apart from the prompt:
    gh issue view 12 | toskarctl ask "Summarize this bug"
  Memory, knowledge, and tools are off unless asked for.
  Exit status: 0 an answer, 1 a model or connection error, 2 usage, 3 an
  answer that doesn't fit --json-schema.

flags:
  --model id          a model, auto (the default), or sai:<slug>
  --profile id        answer as this profile instead
  --system text       a system prompt
  --file path         data to work on, from a file (- for standard input)
  --json-schema path  answer with JSON that fits this JSON Schema
  --max-tokens n      the longest answer, in tokens
  --temperature t     sampling temperature, such as 0 for the steadiest answers
  --no-thinking       a reasoning model answers without thinking first: faster
  --timeout d         how long to wait for the answer (default 5m)
  --memory            let the answer use and add to memory
  --knowledge         let the answer use connected knowledge`

// askIO is where ask reads and writes. stdin is data only when piped.
type askIO struct {
	stdin      io.Reader
	stdinPiped bool
	out        io.Writer
}

// askClient makes the SDK client ask sends with, from TOSKAR_URL and
// TOSKAR_API_KEY as every toskarctl command reads them.
type askClient func(timeout time.Duration) (*toskar.Client, error)

func newAskClient(timeout time.Duration) (*toskar.Client, error) {
	return toskar.New(toskar.WithURL(newDaemon().base), toskar.WithKey(config.Env("API_KEY")), toskar.WithTimeout(timeout))
}

func askCommand(args []string, streams askIO, client askClient) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	fs.SetOutput(new(bytes.Buffer))
	model := fs.String("model", "", "")
	profile := fs.String("profile", "", "")
	system := fs.String("system", "", "")
	file := fs.String("file", "", "")
	schemaPath := fs.String("json-schema", "", "")
	maxTokens := fs.Int("max-tokens", 0, "")
	temperature := fs.Float64("temperature", -1, "")
	noThinking := fs.Bool("no-thinking", false, "")
	timeout := fs.Duration("timeout", 5*time.Minute, "")
	memory := fs.Bool("memory", false, "")
	knowledge := fs.Bool("knowledge", false, "")
	if err := fs.Parse(args); err != nil {
		return usageError(err.Error())
	}
	if *model != "" && *profile != "" {
		return usageError("--model and --profile can't both be given")
	}

	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	data, err := askData(*file, streams)
	if err != nil {
		return &exitError{code: 2, msg: err.Error()}
	}
	message := askdata.Message(prompt, data)
	if message == "" {
		return usageError("nothing to ask: give a prompt, pipe text in, or use --file")
	}

	modelID := "auto"
	switch {
	case *profile != "":
		modelID = "profile:" + *profile
	case *model != "":
		modelID = *model
	}
	var messages []map[string]string
	if *system != "" {
		messages = append(messages, map[string]string{"role": "system", "content": *system})
	}
	messages = append(messages, map[string]string{"role": "user", "content": message})
	body := map[string]any{
		"model":    modelID,
		"messages": messages,
		// A CI step never reads or writes a person's memories unless asked,
		// and never uses tools: what it reads is often untrusted.
		"toskar": map[string]any{"memory": *memory, "knowledge": *knowledge, "tools": []string{}},
	}
	if *maxTokens > 0 {
		body["max_tokens"] = *maxTokens
	}
	if *temperature >= 0 {
		body["temperature"] = *temperature
	}
	if *noThinking {
		// A reasoning model answers without thinking first (#840).
		body["reasoning_effort"] = "none"
	}
	if *schemaPath != "" {
		raw, err := os.ReadFile(*schemaPath)
		if err != nil {
			return &exitError{code: 2, msg: err.Error()}
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			return &exitError{code: 2, msg: fmt.Sprintf("--json-schema %s isn't JSON: %v", *schemaPath, err)}
		}
		body["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "answer", "schema": schema}}
	}
	encoded, _ := json.Marshal(body)

	c, err := client(*timeout)
	if err != nil {
		return &exitError{code: 1, msg: err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	answer, err := toskar.Data(c.OpenaiChatCompletionsWithBodyWithResponse(ctx, "application/json", bytes.NewReader(encoded)))
	if err != nil {
		var e *toskar.APIError
		if errors.As(err, &e) && e.Code == "INVALID_STRUCTURED_OUTPUT" {
			return &exitError{code: 3, msg: e.Message}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return &exitError{code: 1, msg: fmt.Sprintf("no answer within %s (--timeout)", *timeout)}
		}
		var dial *net.OpError
		if errors.As(err, &dial) && dial.Op == "dial" {
			return &exitError{code: 1, msg: (&notRunningError{base: c.BaseURL}).Error()}
		}
		return &exitError{code: 1, msg: err.Error()}
	}
	if len(answer.Choices) == 0 {
		return &exitError{code: 1, msg: "the answer was empty"}
	}
	content := strings.TrimRight(answer.Choices[0].Message.Content, "\n")
	_, err = fmt.Fprintln(streams.out, content)
	return err
}

func usageError(problem string) error {
	return &exitError{code: 2, msg: problem + "\n" + askUsage}
}

// askData is the data to work on: --file, or standard input when piped.
func askData(file string, streams askIO) (string, error) {
	switch {
	case file == "-":
		b, err := readAll(streams.stdin)
		return string(b), err
	case file != "":
		b, err := os.ReadFile(file)
		return string(b), err
	case streams.stdinPiped:
		b, err := readAll(streams.stdin)
		return string(b), err
	}
	return "", nil
}

func readAll(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	return io.ReadAll(r)
}
