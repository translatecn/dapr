package utils

import (
	"fmt"
	"net"
	"strconv"
)

// GetAvailablePort 获取可用端口
func GetAvailablePort() (string, error) {
	address, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:0", "0.0.0.0"))
	if err != nil {
		return "", err
	}

	listener, err := net.ListenTCP("tcp", address)
	if err != nil {
		return "", err
	}

	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port), nil
}

// IsPortAvailable 判断端口是否可以（未被占用）
func IsPortAvailable(port int) bool {
	address := fmt.Sprintf("%s:%d", "0.0.0.0", port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return false
	}

	defer listener.Close()
	return true
}

