package fswatcher

import (
	"context"
	"fmt"
	"log"
	"testing"
)

func TestFsChange(t *testing.T) {
	t.Run("file create", func(t *testing.T) {
		ctx := context.Background()
		eventCh := make(chan struct{}, 4)
		//chan<- struct{}
		go func() {
			err := Watch(ctx, "/tmp", eventCh)
			//assert.NoError(t, err)
			if err != nil {
				log.Fatalln(err)
			}
		}()
		for ev := range eventCh {
			fmt.Println(ev)
			fmt.Println(1111)
		}
	})
}
