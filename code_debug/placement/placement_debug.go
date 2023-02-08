package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

//--log-level info --enable-metrics --replicationFactor 100 --metrics-port 9090 --tls-enabled

func main() {
	fmt.Println(strings.Index("/ba", "/b"))
	// 判断待匹配的字符串是不是  /{.*}
	parameterFinder, _ := regexp.Compile("/{.*}")
	fmt.Println(parameterFinder.MatchString("/{.*}"))
	fmt.Println(url.QueryUnescape("zxczxc"))
	fmt.Println(url.QueryUnescape("https%3A%2F%2Fcong5.net%2Fpost%2Fgolang%3Fname%3D%E5%BC%A0%E4%B8%89%26age%3D20%26sex%3D1"))
}
