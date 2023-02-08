// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package runtime

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

var (
	timeoutSeconds       int    = 60
	requestTimeoutMillis int    = 500
	periodMillis         int    = 100
	urlFormat            string = "http://localhost:%s/v1.0/healthz/outbound"
)
//http://localhost:3500/v1.0/healthz/outbound
//dapr <----> dapr <----> app

func waitUntilDaprOutboundReady(daprHTTPPort string) {
	outboundReadyHealthURL := fmt.Sprintf(urlFormat, daprHTTPPort)
	client := &http.Client{
		Timeout: time.Duration(requestTimeoutMillis) * time.Millisecond,
	}
	println(fmt.Sprintf("Waiting for Dapr to be outbound ready (timeout: %d seconds): url=%s\n", timeoutSeconds, outboundReadyHealthURL))

	var err error
	//到期时间
	timeoutAt := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	// 上一次打印错误的时间
	lastPrintErrorTime := time.Now()
	for time.Now().Before(timeoutAt) {
		err = checkIfOutboundReady(client, outboundReadyHealthURL)
		if err == nil {
			// 成功以http://localhost:3500/v1.0/healthz/outbound 请求 返回204状态码
			println("Dapr is outbound ready!")
			return
		}

		if time.Now().After(lastPrintErrorTime) {
			// 在一秒钟内打印一次错误，以避免过多的错误。
			lastPrintErrorTime = time.Now().Add(time.Second)
			println(fmt.Sprintf("Dapr outbound NOT ready yet: %v", err))
		}
		// 0.1 秒
		time.Sleep(time.Duration(periodMillis) * time.Millisecond)
	}

	println(fmt.Sprintf("timeout waiting for Dapr to become outbound ready. Last error: %v", err))
}

func checkIfOutboundReady(client *http.Client, outboundReadyHealthURL string) error {
	req, err := http.NewRequest(http.MethodGet, outboundReadyHealthURL, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, err = io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != 204 {
		return fmt.Errorf("HTTP status code %v", resp.StatusCode)
	}

	return nil
}
