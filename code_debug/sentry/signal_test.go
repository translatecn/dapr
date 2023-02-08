package sentry_debug

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// signal.Notify 多个signal.Notify 也只会发一次信号
func TestSignal(t *testing.T) {
	fmt.Println(os.Getpid())
	stop := make(chan os.Signal, 22)
	go func() {
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	}()
	go func() {
		signal.Notify(stop, os.Interrupt, syscall.SIGINT)
	}()

	for item := range stop {
		fmt.Println(item.String(), item.Signal)
	}
	time.Sleep(time.Hour)
}
