package main

import (
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const privateKey string = `
-----BEGIN RSA PRIVATE KEY-----
MIICXQIBAAKBgQDEdXD28RmWo8rWJu2FleiAG6wVGy6O0JH0achNiFuFyhf+5AQc
A4KVXaJP5UmeLpYoRIR/Apm10HoE11mPSo/fIaFFbiJc1FfksFBv3QmE4ecbTtpw
v70P9lyr2pBVT4n+TL9Vxu+qLfbraUHA/MLh+csJLILyqkMGP2KAQJhVgQIDAQAB
AoGAb0I/bmpOXoR2K9+x5lRmbp69Ttqs/E5cSjnaKSaPBm7UNhL1zNOkEWkMsgid
L4scmMNs8e0MFe5yG+nFm6PTY8EmrXqH5mBBHlzs8faw9LscVM2+5+JSrfJpAouB
1zlAk6xqTgn9vGxzdu2uFvYf3bcKOsOwsJBjWUCi/H1Q4pECQQDnvu33Phs3uOC/
tylpr52E4mls8WU/9tDahjmXGzCK5u73XULFbm1lzGL1gYGLpxsIHgS2hauuZERx
Av/n8Cc9AkEA2QUUdfqCGiimfhIiNPNCaqIvhQDWPfm2sldMadinysGjDpLe68P5
fuUdOREr6eCwAPC7OVf06sdew+v19cdrlQJBAIBRd/IusWNpOwjsokGiu9WYiEeK
YkXIpFxbdgf1RiujMy5EtXQccPas9R57Vv+8x3r3JCTsXuNxIXRx9MC4eQECQAkn
GbHQGuSXik4O3bp19/sfU/m8C00Z1wa2f9aG+KyodgQLVbOD1GXxq8XYX43BmCqx
/HNyrjWoquqAbSMsgfECQQCT0BenQN1aPwaMY0G6hjlMHp9AtRjy5vFPEP5RtjFj
w816+kr+DSkA68HXk3Wl/C0GD+smA68ZSdL5K8OGQf81
-----END RSA PRIVATE KEY-----
`

const help string = `
appSignTool version1.0.0, command parameter:

appSingTool -f app-name -v SV01.001 -o path
-v: default value SV01.001
-o: default current path

example:
appSignTool -f /usr/local/app/hello -v SV01.001 -o /usr/local/
`
const defAppVersion string = "SV01.001"
const defAppVersionFile string = "version.cfg"
const defAppSignFile string = "sign.cfg"

var gAppPackagePath string

func main() {
	defDir, _ := os.Getwd()
	fn := flag.String("f", "", "app-name absolute path")
	ver := flag.String("v", "SV01.001", "app version")
	out := flag.String("o", defDir, "output file path")
	flag.Parse()

	if len(*fn) < 1 {
		fmt.Println(help)
		return
	}

	if false == checkFileIsExist(*fn) {
		fmt.Printf("%s File not exist.", *fn)
		return
	}

	dir, fl := filepath.Split(*fn)
	_ = dir
	parentPath := fmt.Sprintf("/tmp/app-package%d/", time.Now().Nanosecond())
	gAppPackagePath = fmt.Sprintf("%s%s/", parentPath, fl)
	err := os.MkdirAll(gAppPackagePath, 0755)
	if err != nil {
		fmt.Println("make dir all err: ", err)
		return
	}
	defer os.RemoveAll(parentPath)

	path := filepath.Join(gAppPackagePath, fl)
	_, err = copyFile(path, *fn)
	if err != nil {
		fmt.Println("copy file error: ", err)
		return
	}

	err = genVersionFile(fl, *ver)
	if err != nil {
		return
	}
	err = rsaSign(fl)
	if err != nil {
		return
	}

	cmd := fmt.Sprintf("cd %s && tar -zcvf %s%s.tar %s/", parentPath, *out, fl, fl)
	fmt.Println("cmd: ", cmd)
	execBashCmd(cmd)
}

func checkFileIsExist(filename string) bool {
	var exist = true
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		exist = false
	}
	return exist
}

func getAppHashBytes(name string) []byte {
	path := filepath.Join(gAppPackagePath, name)
	f, err := os.Open(path)
	if err != nil {
		log.Println("getAppHashBytes", err)
		return nil
	}

	defer f.Close()
	md5hash := md5.New()
	if _, err := io.Copy(md5hash, f); err != nil {
		log.Println("getAppHashBytes", err)
		return nil
	}
	return md5hash.Sum(nil)
}

func rsaSign(name string) error {
	data := getAppHashBytes(name)
	h := sha256.New()
	h.Write(data)
	hashed := h.Sum(nil)
	//获取私钥
	block, _ := pem.Decode([]byte(privateKey))
	if block == nil {
		return errors.New("private key error")
	}
	//解析PKCS1格式的私钥
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return err
	}
	sign, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, hashed)

	path := filepath.Join(gAppPackagePath, defAppSignFile)
	f, err := os.Create(path)
	if err != nil {
		fmt.Println("gen sign error: ", err)
		return err
	}

	defer f.Close()
	f.Write(sign)
	f.Sync()

	return nil
}

func copyFile(dstName, srcName string) (written int64, err error) {
	src, err := os.Open(srcName)
	if err != nil {
		return
	}
	defer src.Close()

	dst, err := os.OpenFile(dstName, os.O_WRONLY|os.O_CREATE, 0777)
	if err != nil {
		return
	}
	defer dst.Close()

	return io.Copy(dst, src)
}

func genVersionFile(name, version string) error {
	path := filepath.Join(gAppPackagePath, defAppVersionFile)
	f, err := os.Create(path)
	if err != nil {
		fmt.Println("gen version error: ", err)
		return err
	}

	defer f.Close()
	f.WriteString(version)
	f.Sync()

	return nil
}

func execBashCmd(bash string) string {
	cmd := exec.Command("/bin/sh", "-c", bash)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
