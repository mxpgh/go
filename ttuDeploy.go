package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"ttunodes"
)

var kubeBinInstallPath = "/usr/bin/"
var cniBinInstallPath = "/opt/cni/bin/"
var kubeService = `
[Unit]
Description=kubelet: The Kubernetes Node Agent
Documentation=http://kubernetes.io/docs/

[Service]
ExecStart=/usr/bin/kubelet
Restart=always
StartLimitInterval=0
RestartSec=10

[Install]
WantedBy=multi-user.target
`
var kubeConf = `
[Service]
Environment="KUBELET_KUBECONFIG_ARGS=--bootstrap-kubeconfig=/etc/kubernetes/bootstrap-kubelet.conf --kubeconfig=/etc/kubernetes/kubelet.conf"
Environment="KUBELET_SYSTEM_PODS_ARGS=--pod-manifest-path=/etc/kubernetes/manifests --allow-privileged=true"
Environment="KUBELET_NETWORK_ARGS=--network-plugin=cni --cni-conf-dir=/etc/cni/net.d --cni-bin-dir=/opt/cni/bin"
Environment="KUBELET_DNS_ARGS=--cluster-dns=10.96.0.10 --cluster-domain=cluster.local"
Environment="KUBELET_AUTHZ_ARGS=--authorization-mode=Webhook --client-ca-file=/etc/kubernetes/pki/ca.crt"
Environment="KUBELET_CADVISOR_ARGS=--cadvisor-port=0"
Environment="KUBELET_CGROUP_ARGS=--cgroup-driver=cgroupfs"
Environment="KUBELET_CERTIFICATE_ARGS=--rotate-certificates=true --cert-dir=/var/lib/kubelet/pki"
ExecStart=
ExecStart=/usr/bin/kubelet $KUBELET_KUBECONFIG_ARGS $KUBELET_SYSTEM_PODS_ARGS  $KUBELET_NETWORK_ARGS $KUBELET_DNS_ARGS $KUBELET_AUTHZ_ARGS $KUBELET_CADVISOR_ARGS $KUBELET_CGROUP_ARGS $KUBELET_CERTIFICATE_ARGS $KUBELET_EXTRA_ARGS
`

var helpStr = `
TTU 设备节点组件安装管理(version1.0.0)：
1.安装组件
2.加入集群
3.卸载组件
4.退出
`

func genFile(filePath string, content string) (err error) {
	f, err := os.Create(filePath)
	if err != nil {
		fmt.Printf("Create file error: %s %s\n", filePath, err.Error())
		return
	}

	defer f.Close()
	f.WriteString(content)
	f.Sync()
	return err
}

func copyFile(dstName, srcName string) (written int64, err error) {
	src, err := os.Open(srcName)
	if err != nil {
		return
	}

	defer src.Close()

	dst, err := os.OpenFile(dstName, os.O_WRONLY|os.O_CREATE, os.ModePerm)
	if err != nil {
		return
	}
	defer dst.Close()

	return io.Copy(dst, src)
}

func checkFileIsExist(filename string) bool {
	var exist = true
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		exist = false
	}
	return exist
}

func formatString(str string) string {
	str = strings.Replace(str, " ", "", -1)
	str = strings.Replace(str, "\n", "", -1)
	str = strings.Replace(str, "\r", "", -1)
	return str
}

func hostAddrCheck(addr string) bool {
	items := strings.Split(addr, ":")
	if nil == items || len(items) != 2 {
		return false
	}

	a := net.ParseIP(items[0])
	if nil == a {
		return false
	}

	match, err := regexp.MatchString("^[0-9]*$", items[1])
	if err != nil {
		return false
	}

	i, err := strconv.Atoi(items[1])
	if err != nil {
		return false
	}
	if i < 0 || i > 65535 {
		return false
	}

	if false == match {
		return false
	}

	return true
}

func main() {
	fmt.Println(helpStr)
	f := bufio.NewReader(os.Stdin)
	for {
		fmt.Println("请输入操作选择：")
		input, _ := f.ReadString('\n')
		//fmt.Println(input)
		var sel int
		fmt.Sscan(input, &sel)
		//fmt.Println(sel)
		switch sel {
		case 1:
			//fmt.Println(input)
			err := checkDocker()
			if nil == err {
				checkNetBridge()
				err = installTTUNode()
				if nil == err {
					joinMasterCluster(f)
				}
			}
		case 2:
			//fmt.Println(input)
			joinMasterCluster(f)

		case 3:
			uninstallTTUNode()
		case 4:
			//fmt.Println(input)
			os.Exit(0)
		}
	}
}

func uninstallTTUNode() {
	fmt.Println("正在卸载组件...")
	// 1. exec kubeadm reset
	if checkFileIsExist("/usr/bin/kubeadm") {
		cmd := exec.Command("kubeadm", "reset")
		out, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Println("卸载TTU组件错误 0x0001: ", err.Error())
		} else {
			strOut := string(out)
			fmt.Println(strOut)
		}
	}

	// 2. remove /usr/bin/kubelet /usr/bin/kube-proxy /usr/bin/kubectl /usr/bin/kubeadm
	//	  remove /opt/cni, remove /etc/systemd/system/kubelet.service  /etc/systemd/system/kubelet.service.d
	kubeBinProgs := []string{"kubelet", "kube-proxy", "kubectl", "kubeadm"}
	for _, name := range kubeBinProgs {
		os.RemoveAll(kubeBinInstallPath + name)
	}

	os.RemoveAll("/opt/cni")
	os.RemoveAll("/etc/systemd/system/kubelet.service")
	os.RemoveAll("/etc/systemd/system/kubelet.service.d")
	fmt.Println("卸载完成")
}

func checkHosts() error {
	host, err := os.Hostname()
	if err != nil {
		return nil
	}
	addrs, err := net.LookupIP(host)
	if nil == err {
		for _, addr := range addrs {
			if ipv4 := addr.To4(); ipv4 != nil {
				return nil
			}
		}
	}

	fd, err := os.OpenFile("/etc/hosts", os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil
	}
	defer fd.Close()
	fd.WriteString(string(host + "	127.0.0.1"))
	return nil
}

func checkDocker() error {
	shellCmdStr := `
	MIRROR_URL="http://a58c8480.m.daocloud.io"

	set_daemon_json_file(){
		DOCKER_DAEMON_JSON_FILE="/etc/docker/daemon.json"
		if test -f ${DOCKER_DAEMON_JSON_FILE}
		then
			cp  ${DOCKER_DAEMON_JSON_FILE} "${DOCKER_DAEMON_JSON_FILE}.bak"
			if grep -q registry-mirrors "${DOCKER_DAEMON_JSON_FILE}.bak";then
				cat "${DOCKER_DAEMON_JSON_FILE}.bak" | sed -n "1h;1"'!'"H;\${g;s|\"registry-mirrors\":\s*\[[^]]*\]|\"registry-mirrors\": [\"${MIRROR_URL}\"]|g;p;}" | tee ${DOCKER_DAEMON_JSON_FILE}
			else
				cat "${DOCKER_DAEMON_JSON_FILE}.bak" | sed -n "s|{|{\"registry-mirrors\": [\"${MIRROR_URL}\"],|g;p;" | tee ${DOCKER_DAEMON_JSON_FILE}
			fi
		else
			mkdir -p "/etc/docker"
			echo "{\"registry-mirrors\": [\"${MIRROR_URL}\"]}" | tee ${DOCKER_DAEMON_JSON_FILE}
		fi
	}

	set_daemon_json_file
	systemctl restart docker
`

	cmd := exec.Command("/bin/bash", "-c", "docker -v | awk '{print $3}'")
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("请检测docker是否安装0x0001: %s(%s)\n", string(out), err.Error())
		return err
	}
	fmt.Println("Docker Version: ", string(out))

	cmd = exec.Command("/bin/bash", "-c", shellCmdStr)
	out, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("请检测docker是否安装0x0002: %s(%s)\n", string(out), err.Error())
		return err
	}
	fmt.Println(string(out))

	return nil
}

func checkNetBridge() error {
	shellCmdStr := `
	set_bridge() {
		NET_BRIDGE_CONF_FILE="/etc/sysctl.conf"
		if test -f ${NET_BRIDGE_CONF_FILE}
		then
			cp  ${NET_BRIDGE_CONF_FILE} "${NET_BRIDGE_CONF_FILE}.bak"
			if grep -q net.bridge.bridge-nf-call-ip6tables "${NET_BRIDGE_CONF_FILE}.bak";then
				sed -i 's/net.bridge.bridge-nf-call-ip6tables = 0/net.bridge.bridge-nf-call-ip6tables = 1/g' /etc/sysctl.conf
			else
				sed -i '$a net.bridge.bridge-nf-call-ip6tables = 1' /etc/sysctl.conf
			fi
				if grep -q net.bridge.bridge-nf-call-iptables "${NET_BRIDGE_CONF_FILE}.bak";then
					sed -i 's/net.bridge.bridge-nf-call-iptables = 0/net.bridge.bridge-nf-call-iptables = 1/g' /etc/sysctl.conf
			else
				sed -i '$a net.bridge.bridge-nf-call-iptables = 1' /etc/sysctl.conf
			fi
			rm -rf "${NET_BRIDGE_CONF_FILE}.bak"
		fi
	}
	
	set_bridge
	`

	fmt.Println("正在配置网络...")
	cmd := exec.Command("/bin/bash", "-c", shellCmdStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("网络配置错误0x0001: %s(%s)\n", string(out), err.Error())
		return err
	}
	//fmt.Println(string(out))

	cmd = exec.Command("/bin/bash", "-c", "sysctl -p")
	out, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("网络配置错误0x0002: %s(%s)\n", string(out), err.Error())
		return err
	}
	fmt.Println(string(out))
	fmt.Println("网络配置完成")

	return nil
}

func installTTUNode() error {
	shellCmdStr := `
	chmod +x /tmp/ttu_nodes/node/*
	chmod +x /tmp/ttu_nodes/cni/*
	
	mkdir -p /opt/cni/bin
	`

	defer os.RemoveAll("/tmp/ttu_nodes")

	err := decompress("网络(cni)", "ttu_nodes/cni.tar.gz", "tar -zxvf /tmp/ttu_nodes/cni.tar.gz -C /tmp/ttu_nodes/")
	if err != nil {
		return nil
	}

	err = decompress("images", "ttu_nodes/images.tar.gz", "tar -zxvf /tmp/ttu_nodes/images.tar.gz -C /tmp/ttu_nodes/")
	if err != nil {
		return nil
	}

	err = decompress("管理(kubeadm)", "ttu_nodes/node/kubeadm.tar.gz", "tar -zxvf /tmp/ttu_nodes/node/kubeadm.tar.gz -C /tmp/ttu_nodes/node/")
	if err != nil {
		return nil
	}

	err = decompress("控制(kubelet)", "ttu_nodes/node/kubelet.tar.gz", "tar -zxvf /tmp/ttu_nodes/node/kubelet.tar.gz -C /tmp/ttu_nodes/node/")
	if err != nil {
		return nil
	}

	err = decompress("网络代理(kube-proxy)", "ttu_nodes/node/kube-proxy.tar.gz", "tar -zxvf /tmp/ttu_nodes/node/kube-proxy.tar.gz -C /tmp/ttu_nodes/node/")
	if err != nil {
		return nil
	}

	fmt.Println("正在配置环境")
	err = os.MkdirAll("/etc/systemd/system/kubelet.service.d", os.ModePerm)
	if err != nil {
		fmt.Println("配置环境错误0x0001: ", err.Error())
		return err
	}
	err = genFile("/etc/systemd/system/kubelet.service", kubeService)
	if err != nil {
		fmt.Println("配置环境错误0x0002: ", err.Error())
		return err
	}
	err = genFile("/etc/systemd/system/kubelet.service.d/10-kubeadm.conf", kubeConf)
	if err != nil {
		fmt.Println("配置环境错误0x0003: ", err.Error())
		return err
	}
	err = checkHosts()

	cmd := exec.Command("systemctl enable kubelet.service")
	out, err := cmd.CombinedOutput()
	_ = out
	if err != nil {
		fmt.Printf("配置环境错误0x0004: %s(%s)\n", string(out), err.Error())
		return err
	}

	cmd = exec.Command("/bin/bash", "-c", shellCmdStr)
	out, err = cmd.CombinedOutput()
	_ = out
	if err != nil {
		fmt.Printf("配置环境错误0x0005: %s(%s)\n", string(out), err.Error())
		return err
	}
	fmt.Println("配置环境完成")

	err = installBinFile("网络", "cp -f /tmp/ttu_nodes/cni/* /opt/cni/bin/")
	if err != nil {
		return nil
	}

	cmd = exec.Command("/bin/bash", "-c", "rm -rf /tmp/ttu_nodes/node/*.tar.gz")
	_, err = cmd.CombinedOutput()
	err = installBinFile("管控", "cp -f /tmp/ttu_nodes/node/* /usr/bin/")
	if err != nil {
		return nil
	}

	err = installImages("pause", "docker load < /tmp/ttu_nodes/images/pause-arm_3.0.tar")
	if err != nil {
		return nil
	}

	err = installImages("网络", "docker load < /tmp/ttu_nodes/images/flannel_v0.9.1-arm.tar")
	if err != nil {
		return err
	}

	err = installImages("网络代理", "docker load < /tmp/ttu_nodes/images/kube-proxy-amd64_v1.9.0.tar")
	if err != nil {
		return nil
	}

	fmt.Println("正在清理临时文件...")
	os.RemoveAll("/tmp/ttu_nodes")
	fmt.Println("临时文件清理完成")
	fmt.Println("安装完成")

	return nil
}

func decompress(tip, restore, tar string) error {
	fmt.Printf("正在解压%s组件...\n", tip)
	err := ttunodes.RestoreAsset("/tmp/", restore)
	if err != nil {
		fmt.Printf("组件%s释放失败: %s\n", tip, err.Error())
		return err
	}

	cmd := exec.Command("/bin/bash", "-c", tar)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("组件%s解压失败: %s(%s)\n", tip, string(out), err.Error())
		return err
	}
	fmt.Printf("组件%s解压完成\n", tip)
	return nil
}

func installBinFile(tip, tar string) error {
	fmt.Printf("正在安装%s组件...\n", tip)
	cmd := exec.Command("/bin/bash", "-c", tar)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("安装%s组件错误: %s(%s)\n", tip, string(out), err.Error())
		return err
	}
	fmt.Printf("安装%s组件完成\n", tip)
	return nil
}

func installImages(tip, tar string) error {
	fmt.Printf("正在安装%s镜像...\n", tip)
	cmd := exec.Command("/bin/bash", "-c", tar)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("安装%s镜像错误: %s(%s)\n", tip, string(out), err.Error())
		return err
	}
	fmt.Printf("安装%s镜像完成\n", tip)
	return nil
}

func joinMasterCluster(f *bufio.Reader) error {
	defer os.Exit(0)
	var host string
	for {
		fmt.Println("请输入Master节点IP:Port(格式 IP:Port): ")
		host, _ = f.ReadString('\n')
		host = formatString(host)
		//fmt.Println(host)
		if false == hostAddrCheck(host) {
			fmt.Println("请检测您输入的 IP:Port是否合法? ")
		} else {
			break
		}
	}

	fmt.Println("请输入token: ")
	token, _ := f.ReadString('\n')
	token = formatString(token)
	//fmt.Println(token)

	cmd := exec.Command("/bin/bash", "-c", "kubeadm reset")
	out, err := cmd.CombinedOutput()
	fmt.Println(string(out))
	cmdArgs := fmt.Sprintf("kubeadm join --token %s %s --discovery-token-unsafe-skip-ca-verification", token, host)
	//fmt.Println(cmdArgs)
	cmd = exec.Command("/bin/bash", "-c", cmdArgs)
	out, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("加入集群错误: %s %s\n", string(out), err.Error())
		return err
	}
	fmt.Printf(string(out))

	return nil
}
