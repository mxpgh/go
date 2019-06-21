package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const cfgFile string = "monitor.cfg"
const defAppsFolder string = "/usr/local/apps"

type AppCmdType int8

const (
	_ AppCmdType = iota
	APP_CTL_START
	APP_CTL_STOP
	APP_CTL_ENABLE
	APP_CTL_DISABLE
	APP_CTL_RM
	APP_CTL_LIST
)

var (
	gUnixAddr  *net.UnixAddr
	gUnixConn  *net.UnixConn
	gTaskChan  chan *taskCmd
	gTaskList  []taskItem
	gWaitTime  time.Duration
	gTraceTime time.Time
)

type appCtlCmdReq struct {
	Cmd  AppCmdType
	Name string
	Log  int8
}

type appCtlCmdRsp struct {
	Cmd    AppCmdType
	Name   string
	Result string
	Total  int32
	Items  []appList
}

type srvItem struct {
	Index         int32
	Name          string
	Enable        int8
	Status        int8
	CpuThreshold  int
	CpuUsage      int
	MemThreshold  int
	MemUsage      int
	StartTime     int64
	LogsStartTime int64
	LogsEndTime   int64
}

type appItem struct {
	Index   int32
	Name    string
	Version string
	Hash    string

	SrvTotal int32
	SrvItems []srvItem
}

type appList struct {
	Total int32
	Items []appItem
}

type taskItem struct {
	Pid          int    `json:"pid"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	Cmd          int    `json:"cmd"`
	Enable       int    `json:"enable"`
	StartTime    string `json:"starttime"`
	LogStartTime string `json:"logstarttime`
	LogEndTime   string `json:"logendtime"`
	Param        string `json:"param"`
}

type taskList struct {
	Items []taskItem `json:"items"`
}

type taskCmd struct {
	remote *net.UnixAddr
	req    appCtlCmdReq
}

func main() {
	log.Println("appctl-daemon version1.0.0")

	if _, err := os.Stat(defAppsFolder); os.IsNotExist(err) {
		// 必须分成两步：先创建文件夹、再修改权限
		os.Mkdir(defAppsFolder, os.ModePerm) //0777也可以os.ModePerm
		os.Chmod(defAppsFolder, os.ModePerm)
	}

	gUnixAddr, err := net.ResolveUnixAddr("unixgram", "/var/run/appctl-daemon.sock")
	if err != nil {
		log.Println("resolve addr error: ", err)
		return
	}

	syscall.Unlink("/var/run/appctl-daemon.sock")
	gUnixConn, err = net.ListenUnixgram("unixgram", gUnixAddr)
	if err != nil {
		log.Println("listen error: ", err)
		return
	}
	defer func() {
		gUnixConn.Close()
		//os.Remove("/var/run/appctl-daemon.sock")
	}()

	//处理常见的进程终止信号，以便我们可以正常关闭：
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func(c chan os.Signal) {
		//等待SIGINT或SIGKILL：
		sig := <-c
		log.Println("Caught signal：shutting down ", sig)
		//停止监听（如果unix类型，则取消套接字连接）：
		gUnixConn.Close()
		//os.Remove("/var/run/appctl-daemon.sock")
		//我们完成了：
		os.Exit(0)
	}(sig)

	gTaskChan = make(chan *taskCmd, 100)
	go handleTask()
	go readUnixgram()

	select {}
}

func checkFileIsExist(filename string) bool {
	var exist = true
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		exist = false
	}
	return exist
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

func writeFile(lst *taskList) error {
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
	fd.Sync()

	return nil
}

func loadAppList() {
	lst := taskList{}
	content, err := readFile()
	if err != nil {
		return
	}
	err = json.Unmarshal(content, &lst)
	if err != nil {
		log.Println("loadAppList: ", err)
		return
	}
	gTaskList = append(gTaskList, lst.Items...)
}

func findAppItem(name string) *taskItem {
	for k, v := range gTaskList {
		_ = k
		if v.Name == name {
			return &gTaskList[k]
		}
	}
	return nil
}

func handleTask() {
	loadAppList()

	gWaitTime = 5 * time.Second
	gTraceTime = time.Now().UTC()

	for {
		select {
		case ctlReq, ok := <-gTaskChan:
			{
				if !ok {
					log.Println("chan err")
				} else {
					switch ctlReq.req.Cmd {
					case APP_CTL_START:
						handleAppStart(ctlReq)

					case APP_CTL_STOP:
						handleAppStop(ctlReq)
						log.Println("cmd stop: ", ctlReq.req.Name)

					case APP_CTL_ENABLE:
						handleAppEnable(ctlReq)
						log.Println("cmd enable: ", ctlReq.req.Name)

					case APP_CTL_DISABLE:
						handleAppDisable(ctlReq)
						log.Println("cmd disable: ", ctlReq.req.Name)

					case APP_CTL_RM:
						handleAppRM(ctlReq)
						log.Println("cmd rm: ", ctlReq.req.Name)

					case APP_CTL_LIST:
						handleAppList(ctlReq)
						log.Println("cmd list: ", ctlReq.req.Name)
					}
				}
			}

		case <-time.After(time.Millisecond * 1000):
			{
				checkApps()
			}
		}
	}
}

func checkApps() {
	needUpd := false
	for k, v := range gTaskList {
		_ = k
		//log.Println(v.Path, ",", v.Param, ",", v.Pid)
		if isAlive(v.Pid) {
			ret, err := os.Readlink("/proc/" + strconv.Itoa(v.Pid) + "/exe")
			if err == nil && ret == v.Path {
				//log.Println("link:", ret)
				continue
			} else {
				log.Println("checkApps 0x0002:", err, ",", v.Path)
			}
		}
		cmd := exec.Command(v.Path, v.Param)
		if cmd != nil {
			err := cmd.Start()
			if err != nil {
				log.Println("checkApps 0x0003:", err)
			} else {
				needUpd = true
				gTaskList[k].Pid = cmd.Process.Pid
				log.Println("checkApps start name =", v.Name, ", path =", v.Path, ", pid =", v.Pid)
			}
		} else {
			log.Println("checkApps exec cmd nil:", v.Path)
		}
	}

	if needUpd {
		lst := taskList{}
		lst.Items = append(lst.Items, gTaskList...)
		writeFile(&lst)
	}

	endTime := time.Now().UTC()
	var durationTrace = endTime.Sub(gTraceTime)
	if durationTrace > gWaitTime {
		log.Println("checkApps ok: app list count =", len(gTaskList))
		gTraceTime = time.Now().UTC()
	}
}

func readUnixgram() error {
	for {
		buf := make([]byte, 1400)
		size, remote, err := gUnixConn.ReadFromUnix(buf)
		if err != nil {
			log.Println("readUnixgram error: ", err)
			break
		}

		fmt.Println("recv:", string(buf[:size]), " from ", remote.String())
		gUnixConn.WriteToUnix(buf[:size], remote)

		data := bytes.NewBuffer(buf[:size])
		dec := gob.NewDecoder(data)
		ctlReq := appCtlCmdReq{}
		err = dec.Decode(&ctlReq)
		if err != nil {
			log.Println("decode error: ", err)
			continue
		}

		ctlCmd := &taskCmd{}
		ctlCmd.remote = remote
		ctlCmd.req = ctlReq
		gTaskChan <- ctlCmd
	}
	return nil
}

func handleAppStart(ctl *taskCmd) {
	fn := filepath.Join(defAppsFolder, ctl.req.Name)
	if checkFileIsExist(fn) {
		item := findAppItem(ctl.req.Name)
		if item != nil {
			item.Cmd = 1
		} else {
			item := taskItem{}
			item.Name = ctl.req.Name
			item.Path = fn
			item.Cmd = 1
			item.Enable = 1
			cmd := exec.Command(item.Path, item.Param)
			if cmd != nil {
				err := cmd.Start()
				if err != nil {
					log.Println("checkApps 0x0003:", err)
					gUnixConn.WriteToUnix([]byte("Operation failed"), ctl.remote)
				} else {
					item.Pid = cmd.Process.Pid
					gTaskList = append(gTaskList, item)
					log.Println("checkApps start name =", item.Name, ", path =", item.Path, ", pid =", item.Pid)
					gUnixConn.WriteToUnix([]byte("ok"), ctl.remote)
				}
			} else {
				log.Println("checkApps exec cmd nil:", item.Path)
				gUnixConn.WriteToUnix([]byte("Operation failed"), ctl.remote)
			}
		}

	} else {
		gUnixConn.WriteToUnix([]byte("file not exist"), ctl.remote)
	}
	log.Println("cmd start: ", ctl.req.Name)
}

func handleAppStop(ctl *taskCmd) {

}

func handleAppEnable(ctl *taskCmd) {

}

func handleAppDisable(ctl *taskCmd) {

}

func handleAppRM(ctl *taskCmd) {

}

func handleAppList(ctl *taskCmd) {

}
