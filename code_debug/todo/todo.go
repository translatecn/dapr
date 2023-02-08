package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dapr/dapr/pkg/actors"
	"reflect"
	"sort"
	"time"
)

func main() {
	issue()
}
func issue() {
	data := `{
	"data":{
		"a":"b"
	}
}`
	var a = actors.Reminder{}
	json.Unmarshal([]byte(data), &a)
	var b = actors.CreateReminderRequest{}
	json.Unmarshal([]byte(data), &b)
	//panic: runtime error: comparing uncomparable type map[string]interface {}
	fmt.Println(reflect.DeepEqual(a.Data, b.Data))
	//fmt.Println(a.Data == b.Data)
	c := make(chan int, 0)
	go func() {
		c <- 1
	}()
	fmt.Println(len(c))
	SortSearch()

}

func a() {
	//x()
	fmt.Println(int(^uint(0) >> 1))
	fmt.Println(int64(^uint64(0) >> 1))
	a := []int{1, 2, 3, 4, 5, 6, 7}
	x := sort.Search(len(a), func(i int) bool {
		return a[i] > 3
	})
	fmt.Println(x)
	//b, _ := actors.GetParseTime("0s", nil)
	d, _ := actors.GetParseTime("-0h30m0s", nil)
	fmt.Println(d.Before(time.Now()))
	fmt.Println(time.Now())
	fmt.Println("----")
	//c := time.Now().Add(time.Second)

	//fmt.Println(time.Now().After(time.Now()))
	//c := time.NewTimer(-time.Second * 10)
	//for i := range c.C {
	//	fmt.Println(i)
	//}

	var de map[string][]string
	for item := range de["a"] {
		fmt.Println(item)
	}
	fmt.Println(time.ParseDuration("5s"))

}
func x() {
	ctx, _ := context.WithCancel(context.Background())
	queue := make(chan int)
	select {
	// 处理取消的问题
	case <-ctx.Done():
		return

	case msg := <-queue:
		fmt.Println(msg)
	}
	fmt.Println(123)
}

func SortSearch() {
	arrInts := []int{13, 35, 56, 79}
	findPos := sort.Search(len(arrInts), func(i int) bool {
		return arrInts[i] == 35
	})

	fmt.Println(findPos)
	return
}
