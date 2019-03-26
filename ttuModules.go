package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"ttuModules"
)

var helpStr = `
正准备安装TTU 平台依赖组件(version1.0.0)
`

func main() {
	fmt.Println(helpStr)
	installModules()
}

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

func decompress(tip, restore string) error {
	fmt.Printf("正在解压%s组件...\n", tip)
	err := ttuModules.RestoreAssets("/tmp/", restore)
	if err != nil {
		fmt.Printf("组件%s释放失败: %s\n", tip, err.Error())
		return err
	}

	return nil
}

func installLibFiles(tip, sh string) error {
	fmt.Printf("正在安装%s组件...\n", tip)
	cmd := exec.Command("/bin/bash", "-c", sh)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("安装%s组件错误: %s(%s)\n", tip, string(out), err.Error())
		return err
	}
	fmt.Printf("安装%s组件完成\n", tip)
	return nil
}

func installDebs(sh string) error {
	cmd := exec.Command("/bin/bash", "-c", sh)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("安装组件错误: %s(%s)\n", string(out), err.Error())
		return err
	}
	return nil
}

func installMQTT() error {
	fmt.Printf("正在安装消息组件...\n")
	err := installDebs("dpkg -i /tmp/ttu_modules/packages/libev4_4.04-1_armhf.deb")
	if err != nil {
		return err
	}
	err = installDebs("dpkg -i /tmp/ttu_modules/packages/libuv1_1.8.0-1_armhf.deb")
	if err != nil {
		return err
	}
	err = installDebs("dpkg -i /tmp/ttu_modules/packages/libssl1.0.0_1.0.2g-1ubuntu4.15_armhf.deb")
	if err != nil {
		return err
	}
	err = installDebs("dpkg -i /tmp/ttu_modules/packages/libwebsockets7_1.7.1-1_armhf.deb")
	if err != nil {
		return err
	}
	err = installDebs("dpkg -i /tmp/ttu_modules/packages/mosquitto_1.4.8-1ubuntu0.16.04.6_armhf.deb")
	if err != nil {
		return err
	}

	cmd := exec.Command("/bin/bash", "-c", "pidof mosquitto")
	out, _ := cmd.CombinedOutput()
	//fmt.Println(string(out))
	pid, _ := strconv.Atoi(formatString(string(out)))
	//fmt.Println(pid)
	if pid < 1 {
		cmd := exec.Command("/bin/bash", "-c", "mosquitto -c /etc/mosquitto/mosquitto.conf -v")
		out, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("启动消息组件错误: %s(%s)\n", string(out), err.Error())
		}
		return err
	}

	fmt.Printf("安装消息组件完成\n")
	return nil
}

func installModules() error {
	defer os.RemoveAll("/tmp/ttu_modules")

	err := decompress("数据", "ttu_modules/libs")
	if err != nil {
		return err
	}

	err = installLibFiles("数据", "cp -f /tmp/ttu_modules/libs/* /usr/lib/")
	if err != nil {
		return err
	}

	err = decompress("消息", "ttu_modules/packages")
	if err != nil {
		return err
	}

	err = installMQTT()
	if err != nil {
		return err
	}
	return nil
}
