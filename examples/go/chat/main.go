// Command chat streams an answer, a piece at a time, with the Go SDK.
//
//	go run ./chat "Plan a three-day trip to Juneau"
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	toskar "github.com/yeixio/toskar-sdks/go"
	"github.com/yeixio/toskar-sdks/go/api"
)

func main() {
	message := strings.Join(os.Args[1:], " ")
	if message == "" {
		message = "Hello"
	}
	// This computer's Toskar Core needs no key. Elsewhere, set TOSKAR_URL
	// and TOSKAR_API_KEY, or pass toskar.WithURL and toskar.WithKey.
	c, err := toskar.New()
	if err != nil {
		log.Fatal(err)
	}
	for e, err := range c.ChatStream(context.Background(), api.ChatJSONRequestBody{Message: message}) {
		var apiErr *toskar.APIError
		if errors.As(err, &apiErr) && apiErr.Code == toskar.ErrorCodeNoModelInstalled {
			log.Fatal("Install a model first: open Toskar and choose one in Models.")
		}
		if err != nil {
			log.Fatal(err)
		}
		switch e.Type {
		case "token":
			fmt.Print(e.Content)
		case "done":
			if e.Metrics != nil {
				fmt.Printf("\n\n%d tokens\n", e.Metrics.CompletionTokens)
			}
		}
	}
}
