// Command events prints what happens on Toskar as it happens: models
// loading, answers being written, automations running. Stop with Ctrl-C.
//
//	go run ./events
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"

	toskar "github.com/yeixio/toskar-sdks/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	c, err := toskar.New()
	if err != nil {
		log.Fatal(err)
	}
	for e, err := range c.Events(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		payload, _ := json.Marshal(e.Payload)
		fmt.Println(e.Timestamp.Format("15:04:05"), e.Type, string(payload))
	}
}
