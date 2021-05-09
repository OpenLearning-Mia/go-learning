package main

import (
	"golang.org/x/sync/errgroup"
	"context"
	"sync"
)

func main() {
	var wg sync.WaitGroup
	wg.Add(3)

	ctx, cancel := context.WithCancel(context.Background())

	eg, egCtx := errgroup.WithContext(context.Background())
	eg.Go(getHelloWorldServer(ctx, &wg))
	eg.Go(getHelloNameServer(ctx, &wg))
	eg.Go(getEchoServer(ctx, &wg))

	go func() {
		<-egCtx.Done()
		cancel()
	}()

	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
		<-signals
		cancel()
	}()

	if err := eg.Wait(); err != nil {
		fmt.Printf("error in the server goroutines: %s\n", err)
		os.Exit(1)
	}
	fmt.Println("everything closed successfully")
}
