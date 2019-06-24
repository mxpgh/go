package main

import (
	"bufio"
	"bytes"
	"crypto"
	"crypto/md5"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/gob"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const publicKey string = `
-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDEdXD28RmWo8rWJu2FleiAG6wV
Gy6O0JH0achNiFuFyhf+5AQcA4KVXaJP5UmeLpYoRIR/Apm10HoE11mPSo/fIaFF
biJc1FfksFBv3QmE4ecbTtpwv70P9lyr2pBVT4n+TL9Vxu+qLfbraUHA/MLh+csJ
LILyqkMGP2KAQJhVgQIDAQAB
-----END PUBLIC KEY-----
`

const version string = "1.00"
const cfgFile string = "monitor.cfg"
const defAppVersionFile string = "version.cfg"
const defAppSignFile string = "sign.cfg"
const defAppsFolder string = "/usr/local/apps"
const defAppsExtFolder string = "/usr/local/extapps"

type AppCmdType int8

const (
	_ AppCmdType = iota
	APP_CTL_INSTALL
	APP_CTL_START
	APP_CTL_STOP
	APP_CTL_ENABLE
	APP_CTL_DISABLE
	APP_CTL_RM
	APP_CTL_LIST
	APP_CTL_VERSION
)

const (
	_ AppCmdType = iota
	APP_CMD_START
	APP_CMD_STOP
)

const (
	_ AppCmdType = iota
	APP_STATUS_RUNNING
	APP_STATUS_STOP
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
	Code   int16
	Result string
	Total  int32
	Items  []appItem
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

type taskItem struct {
	Pid          int    `json:"pid"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	Cmd          int    `json:"cmd"`
	Status       int    `json:"status"`
	Enable       int    `json:"enable"`
	StartTime    int64  `json:"starttime"`
	LogStartTime int64  `json:"logstarttime`
	LogEndTime   int64  `json:"logendtime"`
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
	log.Println("appctl-daemon version ", version)

	if _, err := os.Stat(defAppsFolder); os.IsNotExist(err) {
		// 必须分成两步：先创建文件夹、再修改权限
		os.Mkdir(defAppsFolder, 0755)
		oldMask := syscall.Umask(0)
		os.Chmod(defAppsFolder, 0755)
		syscall.Umask(oldMask)
	}

	if _, err := os.Stat(defAppsExtFolder); os.IsNotExist(err) {
		// 必须分成两步：先创建文件夹、再修改权限
		os.Mkdir(defAppsExtFolder, 0755)
		oldMask := syscall.Umask(0)
		os.Chmod(defAppsExtFolder, 0755)
		syscall.Umask(oldMask)
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

	fd, err := os.OpenFile(cfgFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
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

func findAppList(name string) []taskItem {
	var itemList []taskItem
	for k, v := range gTaskList {
		_ = k
		if v.Name == name {
			itemList = append(itemList, gTaskList[k])
		}
	}
	return itemList
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
					case APP_CTL_INSTALL:
						handleAppInstall(ctlReq)

					case APP_CTL_START:
						handleAppStart(ctlReq)

					case APP_CTL_STOP:
						handleAppStop(ctlReq)

					case APP_CTL_ENABLE:
						handleAppEnable(ctlReq)

					case APP_CTL_DISABLE:
						handleAppDisable(ctlReq)

					case APP_CTL_RM:
						handleAppRM(ctlReq)

					case APP_CTL_LIST:
						handleAppList(ctlReq)

					case APP_CTL_VERSION:
						handleAppVersion(ctlReq)
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
		v.Param = ""
		//log.Println(v.Path, ",", v.Param, ",", v.Pid)
		if isAlive(v.Pid) {
			ret, err := os.Readlink("/proc/" + strconv.Itoa(v.Pid) + "/exe")
			if err == nil && ret == v.Path {
				gTaskList[k].Status = int(APP_STATUS_RUNNING)
				//log.Println("link:", ret)
				continue
			} else {
				log.Println("checkApps 0x0001:", err, ",", v.Path)
			}
		}

		if v.Cmd == int(APP_CMD_START) {
			cmd := exec.Command(v.Path, v.Param)
			if cmd != nil {
				err := cmd.Start()
				if err != nil {
					log.Println("checkApps 0x0002:", err)
				} else {
					needUpd = true
					gTaskList[k].Pid = cmd.Process.Pid
					gTaskList[k].Status = int(APP_STATUS_RUNNING)
					gTaskList[k].StartTime = time.Now().Unix()
					gTaskList[k].LogEndTime = time.Now().Unix()
					log.Println("checkApps start name =", v.Name, ", path =", v.Path, ", pid =", v.Pid)
				}
			} else {
				log.Println("checkApps exec cmd nil:", v.Path)
			}
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

		//fmt.Println("recv:", string(buf[:size]), " from ", remote.String())
		//gUnixConn.WriteToUnix(buf[:size], remote)

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

func handleAppInstall(ctl *taskCmd) {
	fn := filepath.Join(defAppsFolder, ctl.req.Name)
	log.Println("handleAppInstall: ", fn)

	if false == checkFileIsExist(fn) {
		writeCtlSimpleRsp(ctl, 1, "File "+ctl.req.Name+" not exist.")
		return
	}
	cmd := exec.Command("/bin/sh", "-c", "tar -zxvf "+fn+" -C "+defAppsExtFolder)
	out, err := cmd.CombinedOutput()
	_ = out
	if err != nil {
		log.Printf("handleAppInstall: %s(%s)\n", string(out), err)
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		return
	}
	writeCtlSimpleRsp(ctl, 0, "Success.")
}

func handleAppStart(ctl *taskCmd) {
	path := filepath.Join(defAppsExtFolder, ctl.req.Name)
	fn := filepath.Join(path, ctl.req.Name)
	log.Println("handleAppStart: ", fn)

	if false == checkFileIsExist(fn) {
		writeCtlSimpleRsp(ctl, 1, "File "+ctl.req.Name+" not exist.")
		return
	}

	item := findAppItem(ctl.req.Name)
	if item != nil {
		item.Cmd = int(APP_CMD_START)
	} else {
		item := taskItem{}
		item.Name = ctl.req.Name
		item.Path = fn
		item.Cmd = int(APP_CMD_START)
		item.Enable = 1
		cmd := exec.Command(item.Path, item.Param)
		if cmd != nil {
			err := cmd.Start()
			if err != nil {
				log.Println("checkApps 0x0003:", err)
				writeCtlSimpleRsp(ctl, 1, "Operation failed.")
			} else {
				item.Pid = cmd.Process.Pid
				item.StartTime = time.Now().Unix()
				item.LogStartTime = time.Now().Unix()
				item.LogEndTime = time.Now().Unix()
				gTaskList = append(gTaskList, item)
				log.Println("checkApps start name =", item.Name, ", path =", item.Path, ", pid =", item.Pid)
				writeCtlSimpleRsp(ctl, 0, "Success.")
			}
		} else {
			log.Println("checkApps exec cmd nil:", item.Path)
			writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		}
	}
}

func handleAppStop(ctl *taskCmd) {
	path := filepath.Join(defAppsExtFolder, ctl.req.Name)
	fn := filepath.Join(path, ctl.req.Name)
	log.Println("handleAppStop: ", fn)

	if false == checkFileIsExist(fn) {
		writeCtlSimpleRsp(ctl, 1, "File "+ctl.req.Name+" not exist.")
		return
	}

	item := findAppItem(ctl.req.Name)
	if item != nil {
		item.Cmd = int(APP_CMD_STOP)
		item.LogEndTime = time.Now().Unix()
		err := syscall.Kill(item.Pid, 9)
		if err != nil {
			writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		} else {
			writeCtlSimpleRsp(ctl, 0, "Success.")
		}

	} else {
		item := taskItem{}
		item.Name = ctl.req.Name
		item.Path = fn
		item.Cmd = int(APP_CMD_STOP)
		item.Enable = 1
		item.LogStartTime = time.Now().Unix()
		item.LogEndTime = time.Now().Unix()
		gTaskList = append(gTaskList, item)
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
	}
}

func handleAppEnable(ctl *taskCmd) {
	path := filepath.Join(defAppsExtFolder, ctl.req.Name)
	fn := filepath.Join(path, ctl.req.Name)
	log.Println("handleAppEnable: ", fn)

	if false == checkFileIsExist(fn) {
		writeCtlSimpleRsp(ctl, 1, "File "+ctl.req.Name+" not exist.")
		return
	}

	item := findAppItem(ctl.req.Name)
	if item != nil {
		item.Enable = 1
		item.LogEndTime = time.Now().Unix()
	} else {
		item := taskItem{}
		item.Name = ctl.req.Name
		item.Path = fn
		item.Enable = 1
		item.LogStartTime = time.Now().Unix()
		item.LogEndTime = time.Now().Unix()
		gTaskList = append(gTaskList, item)
	}

	oldMask := syscall.Umask(0)
	err := os.Chmod(fn, 0755)
	syscall.Umask(oldMask)
	if err != nil {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
	} else {
		writeCtlSimpleRsp(ctl, 0, "Success.")
	}
}

func handleAppDisable(ctl *taskCmd) {
	path := filepath.Join(defAppsExtFolder, ctl.req.Name)
	fn := filepath.Join(path, ctl.req.Name)
	log.Println("handleAppDisable: ", fn)

	if false == checkFileIsExist(fn) {
		writeCtlSimpleRsp(ctl, 1, "File "+ctl.req.Name+" not exist.")
		return
	}

	item := findAppItem(ctl.req.Name)
	if item != nil {
		item.Enable = 0
		item.LogEndTime = time.Now().Unix()
	} else {
		item := taskItem{}
		item.Name = ctl.req.Name
		item.Path = fn
		item.Enable = 0
		item.LogStartTime = time.Now().Unix()
		item.LogEndTime = time.Now().Unix()
		gTaskList = append(gTaskList, item)
	}

	oldMask := syscall.Umask(0)
	err := os.Chmod(fn, 0644)
	syscall.Umask(oldMask)
	if err != nil {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
	} else {
		writeCtlSimpleRsp(ctl, 0, "Success.")
	}
}

func handleAppRM(ctl *taskCmd) {
	path := filepath.Join(defAppsExtFolder, ctl.req.Name)
	fn := filepath.Join(path, ctl.req.Name)
	log.Println("handleAppRM: ", fn)

	if false == checkFileIsExist(fn) {
		writeCtlSimpleRsp(ctl, 1, "File "+ctl.req.Name+" not exist.")
		return
	}

	code := int16(1)
	ret := ""
	item := findAppItem(ctl.req.Name)
	if item != nil {
		if item.Status == int(APP_STATUS_RUNNING) {
			syscall.Kill(item.Pid, 9)
		}
	} else {
		code = 1
		ret = "Operation failed."
	}

	err := os.RemoveAll(path)
	if err != nil {
		code = 1
		ret = "Operation failed."
	} else {
		code = 0
		ret = "Success."
	}

	removeItem(item)
	writeCtlSimpleRsp(ctl, code, ret)
}

func handleAppList(ctl *taskCmd) {
	path := filepath.Join(defAppsExtFolder, ctl.req.Name)
	fn := filepath.Join(path, ctl.req.Name)
	log.Println("handleAppList: ", fn)

	appMap := make(map[string][]taskItem)
	for k, v := range gTaskList {
		_ = k
		appMap[v.Name] = append(appMap[v.Name], v)
	}

	if len(ctl.req.Name) > 0 {
		if false == checkFileIsExist(fn) {
			writeCtlSimpleRsp(ctl, 1, "File "+ctl.req.Name+" not exist.")
			return
		}

		rsp := &appCtlCmdRsp{}
		rsp.Cmd = ctl.req.Cmd
		rsp.Name = ctl.req.Name
		rsp.Code = 0
		rsp.Result = "Success."
		rsp.Total = int32(len(appMap))

		var srvList []srvItem
		itemList := findAppList(ctl.req.Name)
		for k, v := range itemList {
			_ = k
			item := srvItem{}
			item.Index = int32(k)
			item.Name = "srv" + strconv.Itoa(k)
			item.Enable = int8(v.Enable)
			item.Status = int8(v.Status)
			item.CpuThreshold = 90
			item.CpuUsage = getAppCPU(v.Name, v.Pid)
			item.MemThreshold = 90
			item.MemUsage = getAppMem(v.Name, v.Pid)
			item.StartTime = v.StartTime
			if ctl.req.Log == 1 {
				item.LogsStartTime = v.LogStartTime
				item.LogsEndTime = v.LogEndTime
			} else {
				item.LogsStartTime = 0
				item.LogsEndTime = 0
			}

			srvList = append(srvList, item)
		}
		appitem := appItem{}
		appitem.Index = 0
		appitem.Name = ctl.req.Name
		appitem.Version = getAppVersion(ctl.req.Name)
		appitem.Hash = getAppHash(ctl.req.Name)
		appitem.SrvTotal = int32(len(srvList))
		appitem.SrvItems = srvList

		rsp.Items = append(rsp.Items, appitem)
		writeCtlRsp(rsp, ctl.remote)
		return
	}

	rsp := &appCtlCmdRsp{}
	rsp.Cmd = ctl.req.Cmd
	rsp.Name = ctl.req.Name
	rsp.Code = 0
	rsp.Result = "Success."
	rsp.Total = int32(len(appMap))

	for mK, mV := range appMap {
		_ = mK
		var srvList []srvItem
		for k, v := range mV {
			_ = k
			item := srvItem{}
			item.Index = int32(k)
			item.Name = "srv" + strconv.Itoa(k)
			item.Enable = int8(v.Enable)
			item.Status = int8(v.Status)
			item.CpuThreshold = 90
			item.CpuUsage = getAppCPU(v.Name, v.Pid)
			item.MemThreshold = 90
			item.MemUsage = getAppMem(v.Name, v.Pid)
			item.StartTime = v.StartTime
			if ctl.req.Log == 1 {
				item.LogsStartTime = v.LogStartTime
				item.LogsEndTime = v.LogEndTime
			} else {
				item.LogsStartTime = 0
				item.LogsEndTime = 0
			}

			srvList = append(srvList, item)
		}
		appitem := appItem{}
		appitem.Index = 0
		appitem.Name = mK
		appitem.Version = getAppVersion(mK)
		appitem.Hash = getAppHash(mK)
		appitem.SrvTotal = int32(len(srvList))
		appitem.SrvItems = srvList

		rsp.Items = append(rsp.Items, appitem)
	}

	writeCtlRsp(rsp, ctl.remote)
}

func handleAppVersion(ctl *taskCmd) {
	log.Println("handleAppVersion: ", ctl.req.Name)
	if ctl.req.Name == "container" {
		writeCtlSimpleRsp(ctl, 0, version)
	} else {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
	}
}

func writeCtlSimpleRsp(ctl *taskCmd, code int16, ret string) {
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	rsp := &appCtlCmdRsp{}
	rsp.Cmd = ctl.req.Cmd
	rsp.Name = ctl.req.Name
	rsp.Code = code
	rsp.Result = ret
	err := enc.Encode(rsp)
	if err != nil {
		log.Println("writeCtlRsp gob encode error: ", err)
		return
	}
	_, err = gUnixConn.WriteToUnix(buf.Bytes(), ctl.remote)
	if err != nil {
		log.Println("writeCtlRsp error: ", err)
		return
	}
}

func writeCtlRsp(rsp *appCtlCmdRsp, remote *net.UnixAddr) {
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	err := enc.Encode(rsp)
	if err != nil {
		log.Println("writeCtlRsp gob encode error: ", err)
		return
	}
	_, err = gUnixConn.WriteToUnix(buf.Bytes(), remote)
	if err != nil {
		log.Println("writeCtlRsp error: ", err)
		return
	}
}

func removeItem(item *taskItem) bool {
	for i := 0; i < len(gTaskList); i++ {
		if gTaskList[i].Name == item.Name {
			gTaskList = append(gTaskList[:i], gTaskList[i+1:]...)
			return true
		}
	}
	return false
}

func getAppCPU(name string, pid int) int {
	cmd := fmt.Sprintf("top -b -n1 | grep %s | grep -v grep | grep %d | awk '{print$9}'", name, pid)
	log.Println("getAppCPU: cmd=", cmd)
	ret := execBashCmd(cmd)
	log.Println("getAppCPU ret=", ret)
	f, err := strconv.ParseFloat(strings.TrimRight(ret, "%"), 32)
	if err != nil {
		return 0
	}
	log.Println("getAppCPU: ", int(f*100))
	return int(f * 100)
}

func getAppMem(name string, pid int) int {
	path := fmt.Sprintf("/proc/%d/status", pid)
	fl, err := os.Open(path)
	if err != nil {
		log.Println("getAppMem 0x0001:", err)
		return 0
	}

	defer fl.Close()
	buf := bufio.NewReader(fl)
	for {
		line, err := buf.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return 0
			}
			return 0
		}
		line = strings.TrimSpace(line)
		strArr := strings.Split(line, ":")
		if strArr[0] == "VmRSS" {
			log.Println("getAppMem: ", strArr[1])
			mem, err := strconv.Atoi(strings.TrimSpace(strings.TrimRight(strArr[1], "kB")))
			if err != nil {
				log.Println("getAppMem error: ", err)
				return 0
			}
			return mem
		}
	}

	return 0
}

func getAppVersion(name string) string {
	path := filepath.Join(defAppsExtFolder, name)
	fn := filepath.Join(path, defAppVersionFile)
	fl, err := os.Open(fn)
	if err != nil {
		log.Println("getAppVersion 0x0001:", err)
		return ""
	}

	defer fl.Close()
	content, err := ioutil.ReadAll(fl)
	if err != nil {
		log.Println("getAppVersion 0x0002:", err)
		return ""
	}

	return formatString(string(content))
}

func getAppHash(name string) string {
	path := filepath.Join(defAppsExtFolder, name)
	fn := filepath.Join(path, name)
	f, err := os.Open(fn)
	if err != nil {
		fmt.Println("getAppHash", err)
		return ""
	}

	defer f.Close()
	md5hash := md5.New()
	if _, err := io.Copy(md5hash, f); err != nil {
		fmt.Println("getAppHash", err)
		return ""
	}

	return fmt.Sprintf("%x", md5hash.Sum(nil))
}

func formatString(str string) string {
	str = strings.Replace(str, " ", "", -1)
	str = strings.Replace(str, "\n", "", -1)
	str = strings.Replace(str, "\r", "", -1)
	return str
}

func execBashCmd(bash string) string {
	cmd := exec.Command("/bin/sh", "-c", bash)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func getAppHashBytes(name string) []byte {
	path := filepath.Join(defAppsExtFolder, name)
	fn := filepath.Join(path, name)
	f, err := os.Open(fn)
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

func getAppSign(name string) []byte {
	path := filepath.Join(defAppsExtFolder, name)
	fn := filepath.Join(path, defAppSignFile)
	fl, err := os.Open(fn)
	if err != nil {
		log.Println("getAppSign:", err)
		return nil
	}

	defer fl.Close()
	content, err := ioutil.ReadAll(fl)
	if err != nil {
		log.Println("getAppSign:", err)
		return nil
	}
	return content
}

func rsaSignVerify(name string) bool {
	data := getAppHashBytes(name)
	hashed := sha256.Sum256(data)
	block, _ := pem.Decode([]byte(publicKey))
	if block == nil {
		log.Println("rsaSignVerify: public key error")
		return false
	}
	// 解析公钥
	pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		log.Println("rsaSignVerify: parse public key error: ", err)
		return false
	}

	signature := getAppSign(name)
	// 类型断言
	pub := pubInterface.(*rsa.PublicKey)
	//验证签名
	err = rsa.VerifyPKCS1v15(pub, crypto.SHA256, hashed[:], signature)
	if err != nil {
		log.Println("rsaSignVerify: verify sign error: ", err)
		return false
	}

	return true
}
