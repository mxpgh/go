package main

import (
	"errors"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var k8sNode = []string{
	"REPOSITORY:TAG",
	"gcr.io/google_containers/pause-amd64:3.0",
	"gcr.io/google_containers/kube-proxy-amd64:v1.9.0",
	"quay.io/coreos/flannel:v0.9.1-amd64",
}

func main() {
	err := checkDocker()
	if err != nil {
		log.Println(err)
	}

	for {
		checkImage()
		log.Println("check docker")
		time.Sleep(time.Millisecond * 100)
	}
}

func formatString(str string) string {
	str = strings.Replace(str, " ", "", -1)
	str = strings.Replace(str, "\n", "", -1)
	str = strings.Replace(str, "\r", "", -1)
	return str
}

func filterImage(str string) error {
	for _, v := range k8sNode {
		if v == str {
			return nil
		}
	}

	return errors.New("invalid image")
}

func checkDocker() error {
	cmd := exec.Command("/bin/bash", "-c", "docker -v | awk '{print $3}'")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("请检测docker是否安装0x0001: %s(%s)\n", string(out), err.Error())
		return err
	}

	log.Println("Docker Version: ", formatString(string(out)))
	trArr := strings.Split(formatString(string(out)), ".")
	//log.Println(trArr, ",", len(trArr))
	if len(trArr) > 1 {
		majVer, _ := strconv.Atoi(trArr[0])
		subVer, _ := strconv.Atoi(trArr[1])
		//log.Println(majVer, ",", subVer)
		if majVer > 16 {
			if subVer > 11 {
				return nil
			}
		}
	}

	return errors.New("docker version lower, docker version at least 17.12.0")
}

func checkImage() error {
	//cmd := exec.Command("/bin/bash", "-c", `docker images | awk '{printf("%s:%s\n",$1,$2)}'`)
	cmd := exec.Command("/bin/bash", "-c", `docker images --format "{{.ID}}\t{{.Repository}}:{{.Tag}}"`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("checkImage 0x0001: ", err)
		return err
	}

	imgMap := make(map[string][]string)
	trArr := strings.Split(string(out), "\n")
	//log.Println(trArr)
	for _, v := range trArr {
		if v == "" || nil == filterImage(v) {
			continue
		}

		//log.Println(v)
		items := strings.Split(string(v), "\t")
		if len(items) > 1 {
			cmd = exec.Command("docker", "trust", "view", items[1])
			out, err = cmd.CombinedOutput()
			if err != nil {
				//log.Println("checkImage 0x0002: ", err)
				//log.Println(string(out))
				imgMap[items[0]] = append(imgMap[items[0]], items[1])
			}
		}
	}

	for k, v := range imgMap {
		//log.Println(k, ",", v)
		if len(v) > 1 {
			for _, img := range v {
				cmd = exec.Command("/bin/bash", "-c", "docker ps -a | grep "+img+" | awk '{print $1}'")
				out, err = cmd.CombinedOutput()
				if err != nil {
					//log.Println("checkImage 0x0004: ", err)
					cmd = exec.Command("/bin/bash", "-c", "docker rmi "+img)
					out, err = cmd.CombinedOutput()
					if err != nil {
						log.Println("checkImage 0x0005: ", err)
					} else {
						log.Println("remove image ", img)
					}
					continue
				}

				containers := string(out)
				log.Println(containers)
				removeImage(containers, img)
			}
		} else {
			cmd = exec.Command("/bin/bash", "-c", "docker ps -a | grep "+k+" | awk '{print $1}'")
			out, err = cmd.CombinedOutput()
			if err != nil {
				//log.Println("checkImage 0x0009: ", err)
				cmd = exec.Command("/bin/bash", "-c", "docker rmi "+k)
				out, err = cmd.CombinedOutput()
				if err != nil {
					log.Println("checkImage 0x0007: ", err)
				} else {
					log.Println("remove image ", v)
				}
				continue
			}

			containers := string(out)
			log.Println(containers)
			removeImage(containers, k)
		}
	}
	return nil
}

func removeImage(containers, img string) error {
	cmd := exec.Command("/bin/bash", "-c", "docker stop "+containers)
	out, err := cmd.CombinedOutput()
	_ = out
	if err != nil {
		//log.Println("checkImage 0x0010: ", err)
	}
	cmd = exec.Command("/bin/bash", "-c", "docker rm "+containers)
	out, err = cmd.CombinedOutput()
	_ = out
	if err != nil {
		//log.Println("checkImage 0x0011: ", err)
	}
	cmd = exec.Command("/bin/bash", "-c", "docker rmi "+img)
	out, err = cmd.CombinedOutput()
	_ = out
	if err != nil {
		log.Println("checkImage 0x0012: ", err)
	} else {
		log.Println("remove image ", img)
	}
	return nil
}
