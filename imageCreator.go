package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var dockerfile = `FROM mxpan/alpine-arm:1.0
`
var cmdArgDesr = `
Usage imageCreator [OPTIONS]
Options:
   -t: docker image 'name:tag' format (default [])
   -f: file absoulte path
 `

func checkFileIsExist(filename string) bool {
	var exist = true
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		exist = false
	}
	return exist
}

// Home returns the home directory for the executing user.
//
// This uses an OS-specific method for discovering the home directory.
// An error is returned if a home directory cannot be detected.
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

func CopyFile(dstName, srcName string) (written int64, err error) {
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

func createDockerFolder() (folder string, err error) {
	homePath, err := Home()
	//fmt.Println(homePath)
	if runtime.GOOS == "windows" {
		if err != nil {
			folder = "C:\\Users\\dell\\AppData\\Local\\images" + strconv.Itoa(time.Now().Nanosecond())
		} else {
			folder = homePath + "\\AppData\\Local\\images" + strconv.Itoa(time.Now().Nanosecond())
		}
	} else {
		folder = "/usr/local/images" + strconv.Itoa(time.Now().Nanosecond())
	}

	err = os.MkdirAll(folder, os.ModePerm)
	if err != nil {
		fmt.Println("Create file folder error: ", err.Error())
		return
	}
	return folder, err
}

func genDockerfile(filePath string, progName string) (err error) {
	f, err := os.Create(filePath)
	if err != nil {
		fmt.Println("Create file error: ", err.Error())
		return
	}

	defer f.Close()
	f.WriteString(dockerfile)
	f.WriteString("COPY ./" + progName + " /home/\n")
	f.WriteString("ENTRYPOINT [\"/home/" + progName + "\"]\n")
	f.Sync()
	return err
}

func main() {
	imgTag := flag.String("t", "", "docker image 'name:tag' format (default [])")
	progPath := flag.String("f", "", "file absoule path")
	flag.Parse()
	//fmt.Println(*imgTag, *progPath)
	if *imgTag == "" || *progPath == "" {
		fmt.Println(cmdArgDesr)
		return
	}

	if !checkFileIsExist(*progPath) {
		fmt.Println("file %s not exist, please check")
		return
	}

	progName := path.Base(*progPath)
	//fmt.Println(progName)
	cmd := exec.Command("docker", "info")
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println("Please check whether or not the docker has run 0x0001: ", err.Error())
		return
	} else {
		strOut := string(out)
		if !strings.Contains(strOut, "Server") {
			fmt.Println("Please check whether or not the docker has run 0x0002: ", string(out))
			return
		}
	}

	fileFolder, err := createDockerFolder()
	if err != nil {
		return
	}
	defer os.RemoveAll(fileFolder)

	var dockerFilePath = fileFolder + "/" + "Dockerfile"
	err = genDockerfile(dockerFilePath, progName)
	if err != nil {
		return
	}

	_, err = CopyFile(fileFolder+"/"+progName, *progPath)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	cmd = exec.Command("docker", "build", "-t", *imgTag, "-f", dockerFilePath, fileFolder)
	out, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Println("docker image build error: ", err.Error())
		return
	}

	tags := strings.Split(*imgTag, ":")
	//fmt.Println(tags)
	if len(tags) > 0 {
		//fmt.Println(tags[0])
		cmdArgs := "docker images | grep " + tags[0]
		cmd = exec.Command("bash", "-c", cmdArgs)
		out, err = cmd.CombinedOutput()
		if err != nil {
			fmt.Println("docker image result: ", err.Error())
		} else {
			strOut := string(out)
			fmt.Println(strOut)
		}
	}

	fmt.Println("docker image build finish")
}
