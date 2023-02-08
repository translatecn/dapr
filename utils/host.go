package utils

import (
	"net"
	"os"

	"github.com/pkg/errors"
)

const (
	// HostIPEnvVar 是覆盖主机选择的IP地址的环境变量。
	HostIPEnvVar = "DAPR_HOST_IP"
)

// GetHostAddress 为主机选择一个有效的出站IP地址。
func GetHostAddress() (string, error) {
	if val, ok := os.LookupEnv(HostIPEnvVar); ok && val != "" {
		return val, nil
	}

	// 使用udp，这样就不会进行握手。
	// 可以使用任何IP，因为没有建立连接，但我们使用了一个已知的DNS IP。
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		// 无法找到一个通过UDP连接，所以我们回退到“旧”的方式:尝试第一个非环回IPv4:
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return "", errors.Wrap(err, "error getting interface IP addresses")
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String(), nil
				}
			}
		}

		return "", errors.New("could not determine host IP address")
	}

	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}


