// WebSocket load testing tool for ClawReach Bridge.
// Usage: go run test/loadtest/ws-loadtest.go -url ws://$(tailscale ip -4):8080 -conns 100 -duration 60s
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

func main() {
	url := flag.String("url", "ws://127.0.0.1:8080", "WebSocket URL to connect to")
	conns := flag.Int("conns", 10, "Number of concurrent connections")
	duration := flag.Duration("duration", 30*time.Second, "Test duration")
	msgInterval := flag.Duration("interval", 1*time.Second, "Message send interval per connection")
	token := flag.String("token", "", "Auth token (optional)")
	flag.Parse()

	fmt.Printf("ClawReach Bridge Load Test\n")
	fmt.Printf("  URL:          %s\n", *url)
	fmt.Printf("  Connections:  %d\n", *conns)
	fmt.Printf("  Duration:     %s\n", *duration)
	fmt.Printf("  Msg interval: %s\n", *msgInterval)
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	// Handle interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		cancel()
	}()

	var (
		connected    atomic.Int64
		sent         atomic.Int64
		received     atomic.Int64
		errors       atomic.Int64
		connectFails atomic.Int64
	)

	targetURL := *url
	if *token != "" {
		targetURL += "?token=" + *token
	}

	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < *conns; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			c, _, err := websocket.Dial(ctx, targetURL, nil)
			if err != nil {
				connectFails.Add(1)
				return
			}
			connected.Add(1)
			defer c.CloseNow()

			// Read goroutine
			go func() {
				for {
					_, _, err := c.Read(ctx)
					if err != nil {
						return
					}
					received.Add(1)
				}
			}()

			// Write loop
			ticker := time.NewTicker(*msgInterval)
			defer ticker.Stop()

			msg := []byte(fmt.Sprintf(`{"type":"loadtest","conn":%d}`, id))
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					err := c.Write(ctx, websocket.MessageText, msg)
					if err != nil {
						errors.Add(1)
						return
					}
					sent.Add(1)
				}
			}
		}(i)
	}

	// Progress reporting
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				elapsed := time.Since(start).Round(time.Second)
				fmt.Printf("[%s] connected=%d sent=%d recv=%d errors=%d connect_fails=%d\n",
					elapsed, connected.Load(), sent.Load(), received.Load(), errors.Load(), connectFails.Load())
			}
		}
	}()

	wg.Wait()
	elapsed := time.Since(start)

	fmt.Println()
	fmt.Println("Results:")
	fmt.Printf("  Duration:        %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  Connected:       %d / %d\n", connected.Load(), *conns)
	fmt.Printf("  Connect fails:   %d\n", connectFails.Load())
	fmt.Printf("  Messages sent:   %d\n", sent.Load())
	fmt.Printf("  Messages recv:   %d\n", received.Load())
	fmt.Printf("  Errors:          %d\n", errors.Load())
	if elapsed.Seconds() > 0 {
		fmt.Printf("  Send rate:       %.1f msg/s\n", float64(sent.Load())/elapsed.Seconds())
		fmt.Printf("  Recv rate:       %.1f msg/s\n", float64(received.Load())/elapsed.Seconds())
	}

	if connectFails.Load() > 0 || errors.Load() > 0 {
		log.Fatal("Load test completed with errors")
	}
}
