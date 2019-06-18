package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

const cfgFile string = "monitor.cfg"

type appItem struct {
	Pid   int    `json:"pid"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	Param string `json:"param"`
}

type appList struct {
	Items []appItem `json:"items"`
}

func main() {
	log.Println("appmoitor version1.0.0")

	go check()

	select {}
}

//0,1,2为系统进程
func isAlive(pid int) bool {
	if pid < 3 {
		return false
	}
	if err := syscall.Kill(pid, 0); err == nil {
		return true
	}
	return false
}

func readFile() ([]byte, error) {
	fl, err := os.Open(cfgFile)
	if err != nil {
		log.Println("readFile 0x0001:", err)
		return nil, err
	}

	defer fl.Close()
	content, err := ioutil.ReadAll(fl)
	if err != nil {
		log.Println("readFile 0x0002:", err)
		return nil, err
	}

	return content, err
}

func writeFile(lst *appList) error {
	data, err := json.Marshal(&lst)
	if err != nil {
		log.Println("writeFile 0x0001:", err)
		return err
	}

	fd, err := os.OpenFile(cfgFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.ModePerm)
	if err != nil {
		log.Println("writeFile 0x0002:", err.Error())
		return err
	}
	defer fd.Close()
	fd.Write(data)

	return nil
}

func check() {
	var waitTime = 5 * time.Second
	startTraceTime := time.Now().UTC()

	for {
		time.Sleep(1 * time.Second)

		lst := appList{}
		content, err := readFile()
		if err != nil {
			continue
		}
		err = json.Unmarshal(content, &lst)
		if err != nil {
			log.Println("check 0x0001:", err)
			continue
		}

		needUpd := false
		for k, v := range lst.Items {
			_ = k
			//log.Println(v.Path, ",", v.Param, ",", v.Pid)
			if isAlive(v.Pid) {
				ret, err := os.Readlink("/proc/" + strconv.Itoa(v.Pid) + "/exe")
				if err == nil && ret == v.Path {
					//log.Println("link:", ret)
					continue
				} else {
					log.Println("check 0x0002:", err, ",", v.Path)
				}
			}
			cmd := exec.Command(v.Path, v.Param)
			if cmd != nil {
				err := cmd.Start()
				if err != nil {
					log.Println("check 0x0003:", err)
				} else {
					needUpd = true
					lst.Items[k].Pid = cmd.Process.Pid
					log.Println("check start name =", v.Name, ", path =", v.Path, ", pid =", v.Pid)
				}
			} else {
				log.Println("check exec cmd nil:", v.Path)
			}
		}

		if needUpd {
			writeFile(&lst)
		}

		endTime := time.Now().UTC()
		var durationTrace = endTime.Sub(startTraceTime)
		if durationTrace > waitTime {
			log.Println("check ok: app list count =", len(lst.Items))
			startTraceTime = time.Now().UTC()
		}
	}
}
