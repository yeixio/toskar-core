// Command pair connects as a device of its own, as the Toskar app does:
// pair once with the code Connect a device shows, then reach the computer
// at home or, with access from anywhere, away from home.
//
//	go run ./pair 192.168.1.20:7331 123456   # once, at home
//	go run ./pair                            # then, from anywhere
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	toskar "github.com/yeixio/toskar-sdks/go"
)

// saved holds the device's key: keep it as a password is kept.
const saved = "device.json"

func main() {
	ctx := context.Background()
	if len(os.Args) == 3 {
		// The computer shows the same short pin next to the code.
		confirm := func(shortPin string) bool {
			fmt.Printf("Does the computer show %s? [y/N] ", shortPin)
			answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			return strings.EqualFold(strings.TrimSpace(answer), "y")
		}
		d, err := toskar.Pair(ctx, os.Args[1], os.Args[2], "Go example", confirm)
		if errors.Is(err, toskar.ErrNotConfirmed) {
			log.Fatal("Not paired: the short pin wasn't confirmed.")
		}
		if err != nil {
			log.Fatal(err)
		}
		b, _ := json.Marshal(d)
		if err := os.WriteFile(saved, b, 0o600); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Paired. It shows in Devices on that computer.")
	}

	b, err := os.ReadFile(saved)
	if err != nil {
		log.Fatal(err)
	}
	var d toskar.Device
	if err := json.Unmarshal(b, &d); err != nil {
		log.Fatal(err)
	}
	c, err := toskar.New(toskar.WithDevice(d))
	if err != nil {
		log.Fatal(err)
	}
	v, err := c.Version(ctx)
	if err != nil {
		log.Fatal(err)
	}
	// home, direct, or relay: the way in it found.
	fmt.Printf("Reached Toskar %s %s.\n", v.Version, c.Path())
}
