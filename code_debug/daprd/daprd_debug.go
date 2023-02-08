package daprd_debug

import (
	"bytes"
	"fmt"
	"github.com/dapr/dapr/code_debug/replace"
	auth "github.com/dapr/dapr/pkg/runtime/security"
	"github.com/dapr/kit/logger"
	"github.com/pkg/errors"
	"io/ioutil"
	raw_k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var log = logger.NewLogger("[-----------SubCommand-------]")

func PRE(
	mode,
	daprHTTPPort,
	daprAPIGRPCPort,
	appPort, appID, controlPlaneAddress, appProtocol, placementServiceHostAddr, config,
	sentryAddress, outputLevel,
	metricPort,
	daprInternalGRPCPort *string,
	appMaxConcurrency,
	daprHTTPMaxRequestSize *int,
	metricEnable,
	enableMTLS *bool,
) {
	if replace.Replace() > 0 {
		return
	}
	*mode = "kubernetes"
	if *mode == "kubernetes" {
		KillProcess(50005)
		go SubCommand([]string{"zsh", "-c", "kubectl port-forward svc/dapr-placement-server -n dapr-system 50005:50005 "})
	}

	go func() {
		// 开启pprof，监听请求
		ip := "127.0.0.1:6060"
		if err := http.ListenAndServe(ip, nil); err != nil {
			fmt.Printf("start failed on %s\n", ip)
		}
	}()

	fmt.Println(os.Getpid())
	KillProcess(3001)
	go SubCommand([]string{"zsh", "-c", "python3 cmd/daprd/daprd.py"})
	*metricEnable = true
	*metricPort = "19090"
	*outputLevel = "info"
	//*controlPlaneAddress = "dapr-api.dapr-system.svc.cluster.local:80"
	*controlPlaneAddress = "dapr-api.dapr-system.svc.cluster.local:6500" // daprd 注入时指定的值
	*placementServiceHostAddr = "dapr-placement-server.dapr-system.svc.cluster.local:50005"
	//*sentryAddress = "dapr-sentry.dapr-system.svc.cluster.local:80"
	*sentryAddress = "dapr-sentry.dapr-system.svc.cluster.local:10080"

	*appProtocol = "http"
	// kubectl port-forward svc/dapr-sentry -n dapr-system 10080:80 &
	*config = "dapr-config" // 注入的时候，就确定了
	*appMaxConcurrency = -1
	*daprHTTPPort = "3500"
	*daprAPIGRPCPort = "50003"
	*daprInternalGRPCPort = "50001"
	*appPort = "3001"

	*daprHTTPMaxRequestSize = -1
	*enableMTLS = true
	KillProcess(6500)
	go SubCommand([]string{"zsh", "-c", "kubectl port-forward svc/dapr-api -n dapr-system 6500:80"})
	KillProcess(10080)
	go SubCommand([]string{"zsh", "-c", "kubectl port-forward svc/dapr-sentry -n dapr-system 10080:80"})

	// 以下证书，是从daprd的环境变量中截取的
	crt := `-----BEGIN CERTIFICATE-----
MIIBxDCCAWqgAwIBAgIQOg2CZq/XGrVCrIREuGi2NjAKBggqhkjOPQQDAjAxMRcw
FQYDVQQKEw5kYXByLmlvL3NlbnRyeTEWMBQGA1UEAxMNY2x1c3Rlci5sb2NhbDAe
Fw0yMTEyMjkwOTQ0MDZaFw0yMjEyMjkwOTU5MDZaMBgxFjAUBgNVBAMTDWNsdXN0
ZXIubG9jYWwwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAATkerE+pfS3N+MT4Hse
C+U71B6vKA+RNXmRyRATVbxgNFOWf/VKQmZZNTtCtn4rAmiwd3B6F0TG116GsnOS
4JPpo30wezAOBgNVHQ8BAf8EBAMCAQYwDwYDVR0TAQH/BAUwAwEB/zAdBgNVHQ4E
FgQUGLzPsmagl21OzMXfXj4EUjLa1FcwHwYDVR0jBBgwFoAUJcLgJCdNouDEOo6c
AOmxvd8MmLEwGAYDVR0RBBEwD4INY2x1c3Rlci5sb2NhbDAKBggqhkjOPQQDAgNI
ADBFAiEAniyvVAqfXuh6W3Sy7hxVfg/NzH1iHG2hjkNMFrBNS8gCIGT1xgSt+DcB
YLohhWUF0vaO89FRpioLgwp9zzhmHqGy
-----END CERTIFICATE-----`

	key := `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIPtyAeDjnHSIlYbrqsRYBURj2e8IaFhUPQ/HCUu2PWUKoAoGCCqGSM49
AwEHoUQDQgAE5HqxPqX0tzfjE+B7HgvlO9QerygPkTV5kckQE1W8YDRTln/1SkJm
WTU7QrZ+KwJosHdwehdExtdehrJzkuCT6Q==
-----END EC PRIVATE KEY-----`

	ca := `-----BEGIN CERTIFICATE-----
MIIB3DCCAYKgAwIBAgIRALX4t6dDqbVYqccazdgAr8MwCgYIKoZIzj0EAwIwMTEX
MBUGA1UEChMOZGFwci5pby9zZW50cnkxFjAUBgNVBAMTDWNsdXN0ZXIubG9jYWww
HhcNMjExMjI5MDk0NDA2WhcNMjIxMjI5MDk1OTA2WjAxMRcwFQYDVQQKEw5kYXBy
LmlvL3NlbnRyeTEWMBQGA1UEAxMNY2x1c3Rlci5sb2NhbDBZMBMGByqGSM49AgEG
CCqGSM49AwEHA0IABPvKrwupGlpIcDKIRcQzl1qbU7FiJgPqcM2W0qxPNaqUEj4D
dkk2cq+r+KVRAORLTd3J0WaiUjJMN8zT/GhsWt+jezB5MA4GA1UdDwEB/wQEAwIC
BDAdBgNVHSUEFjAUBggrBgEFBQcDAQYIKwYBBQUHAwIwDwYDVR0TAQH/BAUwAwEB
/zAdBgNVHQ4EFgQUJcLgJCdNouDEOo6cAOmxvd8MmLEwGAYDVR0RBBEwD4INY2x1
c3Rlci5sb2NhbDAKBggqhkjOPQQDAgNIADBFAiEAqFwRU4bN7oaWi5OLkxOrp+s5
R8Xw6g3KOatUgEct6KYCICK1pAuUa1AA5GPeOkfKkV+Pd2k8wh3AXXzm6LiB/vFo
-----END CERTIFICATE-----`
	nameSpace := "mesoid"
	// kubectl -n mesoid exec -it pod/etcd-0 -- cat /var/run/secrets/kubernetes.io/serviceaccount/token

	taskId := "61c2cb20562850d49d47d1c7"
	//
	*appID = "app01"

	KillProcess(50001)
	go SubCommand([]string{"zsh", "-c", "kubectl port-forward svc/dp-" + taskId + "-executorapp-dapr -n " + nameSpace + " 50001:50001"})
	//*appID = "ls-demo"  // 不能包含.
	UpdateHosts(fmt.Sprintf("dp-%s-executorapp-dapr.%s.svc.cluster.local", taskId, nameSpace))

	command := exec.Command("zsh", "-c", fmt.Sprintf("kubectl -n %s get pods|grep worker |grep %s |grep Running|awk 'NR==1'|awk '{print $1}'", nameSpace, taskId))
	podNameBytes, _ := command.CombinedOutput()
	podName := strings.Trim(string(podNameBytes), "\n")

	getToken := fmt.Sprintf("kubectl -n mesoid exec -it pod/%s -c worker-agent -- cat /var/run/secrets/kubernetes.io/serviceaccount/token > /tmp/token", podName)
	go SubCommand([]string{"zsh", "-c", getToken})
	os.Setenv("DAPR_CERT_CHAIN", crt)
	os.Setenv("DAPR_CERT_KEY", key)
	//ca
	os.Setenv("DAPR_TRUST_ANCHORS", ca)

	os.Setenv("NAMESPACE", "mesoid")

	rootCertPath := "/tmp/ca.crt"
	issuerCertPath := "/tmp/issuer.crt"
	issuerKeyPath := "/tmp/issuer.key"

	_ = ioutil.WriteFile(rootCertPath, []byte(ca), 0644)
	_ = ioutil.WriteFile(issuerCertPath, []byte(crt), 0644)
	_ = ioutil.WriteFile(issuerKeyPath, []byte(key), 0644)

	os.Setenv("NAMESPACE", nameSpace)
	os.Setenv("SENTRY_LOCAL_IDENTITY", nameSpace+":default") // 用于验证 token 是不是这个sa的,注入的时候指定的
	*auth.GetKubeTknPath() = "/tmp/token"
	KillProcess(45454)
	go SubCommand([]string{"zsh", "-c", "kubectl port-forward svc/dapr-redis-svc -n " + nameSpace + " 45454:45454"})
	time.Sleep(time.Second * 14)
}

// GetK8s 此处改用加载本地配置文件 ~/.kube/config
func GetK8s() *raw_k8s.Clientset {
	conf, err := rest.InClusterConfig()
	if err != nil {
		// 路径直接写死
		conf, err = clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
		if err != nil {
			panic(err)
		}
	}

	kubeClient, _ := raw_k8s.NewForConfig(conf)
	return kubeClient
}

func UpdateHosts(domain string) {
	file, err := ioutil.ReadFile("/etc/hosts_bak")
	if err != nil {
		panic(err)
	}
	change := string(file) + "\n127.0.0.1                    " + domain + "\n"
	change += "\n127.0.0.1                    " + "dapr-redis-svc" + "\n"
	err = ioutil.WriteFile("/etc/hosts", []byte(change), 777)
	if err != nil {
		panic(err)
	}
}

func KillProcess(port int) {
	command := exec.Command("zsh", "-c", "kill-port.sh "+strconv.Itoa(port))
	err := command.Run()
	if err != nil {
		panic(err)
	}
}

func SubCommand(opt []string) {
	cmd := exec.Command(opt[0], opt[1:]...)
	// 命令的错误输出和标准输出都连接到同一个管道
	stdout, err := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	if err != nil {
		panic(err)
	}
	if err = cmd.Start(); err != nil {
		panic(err)
	}
	for {
		tmp := make([]byte, 1024)
		_, err := stdout.Read(tmp)
		res := strings.Split(string(tmp), "\n")
		for _, v := range res {
			if v[0] != '\u0000' {
				fmt.Println(v)
				//log.Debug(v)
			}
		}
		if err != nil {
			break
		}
	}

	//if err = cmd.Wait(); err != nil {
	//	log.Fatal(err)
	//}
}

func Home() (string, error) {
	user, err := user.Current()
	if nil == err {
		return user.HomeDir, nil
	}

	// cross compile support

	if "windows" == runtime.GOOS {
		return homeWindows()
	}

	// Unix-like system, so just assume Unix
	return homeUnix()
}

func homeUnix() (string, error) {
	// First prefer the HOME environmental variable
	if home := os.Getenv("HOME"); home != "" {
		return home, nil
	}

	// If that fails, try the shell
	var stdout bytes.Buffer
	cmd := exec.Command("sh", "-c", "eval echo ~$USER")
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}

	result := strings.TrimSpace(stdout.String())
	if result == "" {
		return "", errors.New("blank output when reading home directory")
	}

	return result, nil
}

func homeWindows() (string, error) {
	drive := os.Getenv("HOMEDRIVE")
	path := os.Getenv("HOMEPATH")
	home := drive + path
	if drive == "" || path == "" {
		home = os.Getenv("USERPROFILE")
	}
	if home == "" {
		return "", errors.New("HOMEDRIVE, HOMEPATH, and USERPROFILE are blank")
	}

	return home, nil
}
