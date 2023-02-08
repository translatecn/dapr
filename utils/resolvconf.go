package utils

import (
	"bufio"
	"bytes"
	"os"
	"regexp"
	"sort"
	"strings"
)

const (
	// DefaultKubeClusterDomain 是KubeClusterDomain的默认值。
	DefaultKubeClusterDomain = "cluster.local"
	defaultResolvPath        = "/etc/resolv.conf"
	commentMarker            = "#"
)

//root@dp-618b5e4aa5ebc3924db86860-task-daemonapp-ddea4-bbfc676545fd96:/app# cat /etc/resolv.conf
//nameserver 10.96.0.10
//search mesoid.svc.cluster.local svc.cluster.local cluster.local
//options ndots:5

var searchRegexp = regexp.MustCompile(`^\s*search\s*(([^\s]+\s*)*)$`)

// GetKubeClusterDomain 在/etc/resolv.conf文件中搜索KubeClusterDomain的值。
func GetKubeClusterDomain() (string, error) {
	resolvContent, err := getResolvContent(defaultResolvPath)
	if err != nil {
		return "", err
	}
	return getClusterDomain(resolvContent)
}

func getClusterDomain(resolvConf []byte) (string, error) {
	var kubeClusterDomian string
	searchDomains := getResolvSearchDomains(resolvConf)
	sort.Strings(searchDomains)
	if len(searchDomains) == 0 || searchDomains[0] == "" {
		kubeClusterDomian = DefaultKubeClusterDomain
	} else {
		kubeClusterDomian = searchDomains[0]
	}
	return kubeClusterDomian, nil
}

func getResolvContent(resolvPath string) ([]byte, error) {
	var _ = `nameserver 10.96.0.10
search a.svc.cluster.local svc.cluster.local cluster.local
options ndots:5`
	return os.ReadFile(resolvPath)
	//return []byte(text), nil
}

func getResolvSearchDomains(resolvConf []byte) []string {
	var (
		domains []string
		lines   [][]byte
	)

	scanner := bufio.NewScanner(bytes.NewReader(resolvConf))
	// 去掉注释内容
	for scanner.Scan() {
		line := scanner.Bytes()
		commentIndex := bytes.Index(line, []byte(commentMarker))
		if commentIndex == -1 {
			lines = append(lines, line)
		} else {
			// name = 1 # 名称
			lines = append(lines, line[:commentIndex])
		}
	}

	for _, line := range lines {
		match := searchRegexp.FindSubmatch(line)
		if match == nil {
			continue
		}
		// 按空格切分
		domains = strings.Fields(string(match[1]))
	}

	return domains
}
