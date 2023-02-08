package main

import (
	"bufio"
	"bytes"
	"context"
	"contrib.go.opencensus.io/exporter/prometheus"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

//https://opencensus.io/quickstart/go/metrics/

//创建指标
var (
	MLatencyMs = stats.Float64("repl/latency", "The latency in milliseconds per REPL loop", "ms")
	//mesoid_demo_latency_bucket{method="repl",le="75"} 14
	MLineLengths = stats.Int64("repl/line_lengths", "The distribution of line lengths", "By")
)

var (
	//创建标签
	KeyMethod, _ = tag.NewKey("method")
	KeyStatus, _ = tag.NewKey("status")
	KeyError, _  = tag.NewKey("error")
)

var (
	//创建视图
	LatencyView = &view.View{
		Name:       MLatencyMs.Name(),
		Measure:     MLatencyMs, // 指标
		Description: MLatencyMs.Description(),
		//以桶为单位的延时
		// [>=0ms, >=25ms, >=50ms, >=75ms, >=100ms, >=200ms, >=400ms, >=600ms, >=800ms, >=1s, >=2s, >=4s, >=6s]
		Aggregation: view.Distribution(0, 25, 50, 75, 100, 200, 400, 600, 800, 1000, 2000, 4000, 6000),
		TagKeys: []tag.Key{
			KeyMethod, KeyStatus, KeyError, // 可以通过这里过滤
		},
	}

	LineCountView = &view.View{
		Name:        "demo/lines_in",
		Measure:     MLineLengths,
		Description: "The number of lines from standard input",
		Aggregation: view.Count(),
	}

	LineLengthView = &view.View{
		Name:        "demo/line_lengths",
		Description: "Groups the lengths of keys in buckets",
		Measure:     MLineLengths,
		// Lengths: [>=0B, >=5B, >=10B, >=15B, >=20B, >=40B, >=60B, >=80, >=100B, >=200B, >=400, >=600, >=800, >=1000]
		Aggregation: view.Distribution(0, 5, 10, 15, 20, 40, 60, 80, 100, 200, 400, 600, 800, 1000),
	}
)

func Register() {
	// 注册视图,
	//必须存在此步骤，否则记录的度量将被删除并且永远不会导出。
	if err := view.Register(LatencyView, LineCountView, LineLengthView); err != nil {
		log.Fatalf("Failed to register the views: %v", err)
	}

}

func ExposeHttp() {
	//创建导出器
	pe, err := prometheus.NewExporter(prometheus.Options{
		Namespace: "mesoid",
	})
	if err != nil {
		log.Fatalf("Failed to create the Prometheus stats exporter: %v", err)
	}

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", pe)
		if err := http.ListenAndServe(":8000", mux); err != nil {
			log.Fatalf("Failed to run Prometheus scrape endpoint: %v", err)
		}
	}()
}
func main() {
	Register()
	ExposeHttp()

	br := bufio.NewReader(os.Stdin)

	for {
		if err := readEvaluateProcess(br); err != nil {
			if err == io.EOF {
				return
			}
			log.Fatal(err)
		}
	}
}

func readEvaluateProcess(br *bufio.Reader) (terr error) {
	startTime := time.Now()
	//插入标签
	//mesoid_demo_latency_bucket{method="repl",le="75"} 14
	ctx, err := tag.New(context.Background(), tag.Insert(KeyStatus, "xxxx"), tag.Insert(KeyMethod, "repl"))
	if err != nil {
		return err
	}

	defer func() {
		if terr != nil {
			ctx, _ = tag.New(ctx, tag.Upsert(KeyStatus, "ERROR"), tag.Upsert(KeyError, terr.Error()))
		}
		//记录指标
		stats.Record(ctx, MLatencyMs.M(float64(time.Since(startTime).Nanoseconds())/1e6))
	}()

	line, _, err := br.ReadLine()
	if err != nil {
		if err != io.EOF {
			return err
		}
		log.Fatal(err)
	}

	startTime = time.Now()
	//是记录统计资料时测量的数字值。每个测量值都提供了创建其同类测量值的方法。例如，Int64Measure提供了M，将int64转换成测量值。
	stats.Record(ctx,
		MLatencyMs.M(float64(time.Since(startTime).Nanoseconds())/1e6),
		MLineLengths.M(int64(len(line))),
	)
	out := bytes.ToUpper(line)
	if err != nil {
		return err
	}
	fmt.Printf("< %s\n\n", out)
	return nil
}
