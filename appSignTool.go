package main

import (
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
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
appSignTool version2.0.0, command parameter:

appSingTool -f folder -b app-name -l lib -v SV01.001 -o package name
-f: app program directory
-b: app execute program name
-l: app program dependency library
-v: default value SV01.001
-o: output app package name

example:
appSignTool -f /usr/local/app -b hello -l /usr/local/app/lib -v SV01.001 -o app
`
const defAppVersion string = "SV01.001"
const defAppVersionFile string = "version.cfg"
const defAppSignFile string = "sign.cfg"
const defAppCfgFile string = "app.cfg"

var gAppPackagePath string

type appCfg struct {
	AppName string `json:"appname"`
	BinName string `json:"binname"`
	LibPath string `json:"libpath"`
}

func main() {
	defDir := getCurrentPath()
	fn := flag.String("f", "", "app program absolute path")
	bin := flag.String("b", "", "app execute program name")
	lib := flag.String("l", "", "app program dependency library")
	ver := flag.String("v", "SV01.001", "app version")
	out := flag.String("o", "", "output file")
	flag.Parse()

	if len(*fn) < 1 || len(*bin) < 1 {
		fmt.Println(help)
		return
	}

	binPath := filepath.Join(*fn, *bin)
	if false == pathExists(binPath) {
		fmt.Printf("%s File not exist.", binPath)
		return
	}

	dir, outApp := filepath.Split(*out)
	_ = dir
	parentPath := fmt.Sprintf("/tmp/app-package%d/", time.Now().Nanosecond())
	appName := strings.TrimSuffix(outApp, ".tar")
	gAppPackagePath = fmt.Sprintf("%s%s/", parentPath, appName)
	err := os.MkdirAll(gAppPackagePath, 0755)
	if err != nil {
		fmt.Println("make dir err: ", err)
		return
	}
	err = os.MkdirAll(gAppPackagePath+"/bin", 0755)
	if err != nil {
		fmt.Println("make program dir err: ", err)
		return
	}
	if len(*lib) > 1 {
		err = os.MkdirAll(gAppPackagePath+"/lib", 0755)
		if err != nil {
			fmt.Println("make lib dir err: ", err)
			return
		}
	}

	defer os.RemoveAll(parentPath)

	cfg := appCfg{}
	cfg.AppName = appName
	cfg.BinName = *bin

	progPath := filepath.Join(gAppPackagePath, "bin")
	err = copyDir(*fn, progPath)
	if err != nil {
		fmt.Println("copy program file error: ", err)
		return
	}

	if len(*lib) > 1 {
		libPath := filepath.Join(gAppPackagePath, "lib")
		cfg.LibPath = "lib"
		err = copyDir(*lib, libPath)
		if err != nil {
			fmt.Println("copy dependency library file error: ", err)
			return
		}
	}

	err = genVersionFile(*ver)
	if err != nil {
		return
	}

	err = genCfgFile(&cfg)
	if err != nil {
		return
	}

	binPath = filepath.Join(gAppPackagePath, "bin/"+*bin)
	err = rsaSign(binPath)
	if err != nil {
		return
	}

	cmd := fmt.Sprintf("cd %s && tar -zcvf %s.tar %s/", parentPath, filepath.Join(defDir, *out), appName)
	fmt.Println(cmd)
	execBashCmd(cmd)
}

func getCurrentPath() string {
	execPath, err := exec.LookPath(os.Args[0])
	if err != nil {
		return ""
	}

	// Is Symlink
	fi, err := os.Lstat(execPath)
	if err != nil {
		return ""
	}

	if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
		execPath, err = os.Readlink(execPath)
		if err != nil {
			return ""
		}
	}

	execDir := filepath.Dir(execPath)
	if execDir == "." {
		execDir, err = os.Getwd()
		if err != nil {
			return ""
		}
	}

	return execDir + "/"
}

func pathExists(filename string) bool {
	var exist = true
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		exist = false
	}
	return exist
}

func getAppHashBytes(name string) []byte {
	//path := filepath.Join(gAppPackagePath, name)
	f, err := os.Open(name)
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

/*
func copyFile(dstName, srcName string) (written int64, err error) {
	src, err := os.Open(srcName)
	if err != nil {
		return
	}
	defer src.Close()
	var fm os.FileMode
	fi, err := os.Stat(srcName)
	if err != nil {
		fm = os.FileMode(0777)
	} else {
		fm = fi.Mode()
	}
	dst, err := os.OpenFile(dstName, os.O_WRONLY|os.O_CREATE, fm)
	if err != nil {
		return
	}
	defer dst.Close()

	return io.Copy(dst, src)
}
*/

//生成目录并拷贝文件
func copyFile(src, dest string) (w int64, err error) {
	srcFile, err := os.Open(src)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	defer srcFile.Close()

	//分割path目录
	destSplitPathDirs := strings.Split(dest, "/")

	//检测是否存在目录
	destSplitPath := ""
	for index, dir := range destSplitPathDirs {
		if index < len(destSplitPathDirs)-1 {
			destSplitPath = destSplitPath + dir + "/"

			if false == pathExists(destSplitPath) {
				fmt.Println("create dir:" + destSplitPath)
				//创建目录
				err := os.Mkdir(destSplitPath, 0755)
				if err != nil {
					fmt.Println(err)
				}
			}
		}
	}

	var fm os.FileMode
	fi, err := os.Stat(src)
	if err != nil {
		fm = os.FileMode(0755)
	} else {
		fm = fi.Mode()
	}
	dstFile, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE, fm)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	defer dstFile.Close()

	return io.Copy(dstFile, srcFile)
}

func copyDir(srcPath, destPath string) error {
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		fmt.Println(err.Error())
		return err
	}
	if !srcInfo.IsDir() {
		e := errors.New("srcPath is not correct directory")
		fmt.Println(e.Error())
		return e
	}

	destInfo, err := os.Stat(destPath)
	if err != nil {
		fmt.Println(err.Error())
		return err
	}
	if !destInfo.IsDir() {
		e := errors.New("destPath is not correct directory")
		fmt.Println(e.Error())
		return e
	}

	err = filepath.Walk(srcPath, func(path string, f os.FileInfo, err error) error {
		if f == nil {
			return err
		}
		if !f.IsDir() {
			path := strings.Replace(path, "\\", "/", -1)
			destNewPath := strings.Replace(path, srcPath, destPath, -1)
			fmt.Println("copy file:" + path + " to " + destNewPath)
			copyFile(path, destNewPath)
		}
		return nil
	})
	if err != nil {
		fmt.Printf(err.Error())
	}
	return err
}

func genVersionFile(version string) error {
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

func genCfgFile(cfg *appCfg) error {
	data, err := json.Marshal(&cfg)
	if err != nil {
		log.Println("write app config file masshal error:", err)
		return err
	}

	path := filepath.Join(gAppPackagePath, defAppCfgFile)
	fd, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		log.Println("write app config openfile error:", err.Error())
		return err
	}
	defer fd.Close()
	fd.Write(data)
	fd.Sync()

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
