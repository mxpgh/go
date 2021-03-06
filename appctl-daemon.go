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
	"errors"
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

const version string = "1.31"
const cfgFile string = "monitor.cfg"
const defAppVersionFile string = "version.cfg"
const defAppSignFile string = "sign.cfg"
const defAppCfgFile string = "app.cfg"
const defAppEventFile string = "event.log"
const defAppsFolder string = "/usr/local/apps"
const defAppsExtFolder string = "/usr/local/extapps"
const defCPUThreshold int = 90
const defMemThreshold int = 90
const defCPULimit int = 90
const defMemLimit int = 90

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
	APP_CTL_CONFIG_CPU_THRESHOLD
	APP_CTL_CONFIG_MEM_THRESHOLD
	APP_CTL_QUERY_CPU_THRESHOLD
	APP_CTL_QUERY_MEM_THRESHOLD
	APP_CTL_CONFIG_CPU_LIMIT
	APP_CTL_CONFIG_MEM_LIMIT
	APP_CTL_QUERY_CPU_LIMIT
	APP_CTL_QUERY_MEM_LIMIT
	APP_CTL_QUERY_ALL_RESOURCE
	APP_CTL_LOGS
)

const (
	_ AppCmdType = iota
	APP_CMD_START
	APP_CMD_STOP
)

const (
	_ AppCmdType = iota
	APP_STATUS_INSTALL
	APP_STATUS_RUNNING
	APP_STATUS_STOP
)

var (
	gUnixAddr       *net.UnixAddr
	gUnixConn       *net.UnixConn
	gUDPConn        net.Conn
	gTaskChan       chan *taskCmd
	gTaskList       []taskItem
	gWaitTime       time.Duration
	gTraceTime      time.Time
	gCPUThreshold   int
	gMemThreshold   int
	gAppCurrentPath string
	gContainerID    string
)

type appCtlCmdReq struct {
	Cmd   AppCmdType
	Name  string
	Log   int8
	Value int
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
	CPUThreshold  int
	CPULimit      int
	CPUUsage      int
	MemThreshold  int
	MemLimit      int
	MemUsage      int
	StartTime     int64
	LogsStartTime int64
	LogsEndTime   int64
}

type appItem struct {
	Index    int32
	Name     string
	Version  string
	Hash     string
	SrvTotal int32
	SrvItems []srvItem
	LogFile  string
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
	CPUThreshold int    `json:"cputhreshold"`
	MemThreshold int    `json:"memthreshold"`
	CPULimit     int    `json:"cpulimit"`
	MemLimit     int    `json:"memlimit"`
	CPURate      int    `json:"cpurate"`
	MemRate      int    `json:"memrate"`
	Version      string `json:"version"`
	Hash         string `json:"hash"`
	Param        string `json:"param"`
	LogFile      string `json:"logfile"`
	cfg          appCfg
}

type taskList struct {
	CPUThreshold int        `json:"cputhreshold"`
	MemThreshold int        `json:"memthreshold"`
	Items        []taskItem `json:"items"`
}

type taskCmd struct {
	remote *net.UnixAddr
	req    appCtlCmdReq
}

type warnNotify struct {
	Cid       string `json:"cid"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Value     int    `json:"value"`
	Threshold int    `json:"threshold"`
}

type appCfg struct {
	AppName string `json:"appname"`
	BinName string `json:"binname"`
	LibPath string `json:"libpath"`
}

type appResource struct {
	Name         string `json:"name"`
	CPUThreshold int    `json:"cputhreshold`
	MemThreshold int    `json:"memthreshold"`
}

type appResourceList struct {
	Items []appResource `json:"items"`
}

func main() {
	gAppCurrentPath = getCurrentPath()
	log.Printf("appctl-daemon version %s, path: %s\n", version, gAppCurrentPath)
	gContainerID = getContainerID()
	log.Println("appctl-daemon in container id: ", gContainerID)

	gCPUThreshold = defCPUThreshold
	gMemThreshold = defMemThreshold

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

	gUDPConn, err = net.Dial("udp", "172.17.0.1:5600")
	if err != nil {
		log.Println("net.Dial udp error: ", err.Error())
	} else {
		defer gUDPConn.Close()
	}

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

	gTaskChan = make(chan *taskCmd, 50)
	execBashCmd("tar -zxvf /home/lib.tar.gz -C /")
	//os.Setenv("LD_LIBRARY_PATH", "/lib:/usr/lib:/home/zxlib")
	//log.Println(os.Getenv("LD_LIBRARY_PATH"))

	loadAppList()
	go handleTask()
	go readUnixgram()

	log.Println("appctl-daemon start service")
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
	path := filepath.Join(gAppCurrentPath, cfgFile)
	fl, err := os.Open(path)
	if err != nil {
		log.Println("readFile open:", err)
		return nil, err
	}

	defer fl.Close()
	content, err := ioutil.ReadAll(fl)
	if err != nil {
		log.Println("readFile readall:", err)
		return nil, err
	}

	return content, err
}

func writeFile(lst *taskList) error {
	lst.CPUThreshold = gCPUThreshold
	lst.MemThreshold = gMemThreshold

	data, err := json.Marshal(&lst)
	if err != nil {
		log.Println("writeFile masshal error:", err)
		return err
	}

	path := filepath.Join(gAppCurrentPath, cfgFile)
	fd, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		log.Println("writeFile openfile error:", err.Error())
		return err
	}
	defer fd.Close()
	fd.Write(data)
	fd.Sync()

	return nil
}

func writeAppEventLog(item *taskItem, format string, v ...interface{}) {
	if item == nil {
		log.Println("writeAppEventLog: item nil")
		return
	}
	path := filepath.Join(defAppsExtFolder, item.Name)
	fn := filepath.Join(path, defAppEventFile)
	fd, err := os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		log.Println("writeAppEventLog: ", err.Error())
		return
	}
	defer fd.Close()

	item.LogFile = fn
	str := fmt.Sprintf(format, v...)
	fd.WriteString(time.Unix(time.Now().Unix(), 0).Format("2006-01-02 15:04:05") + " " + str + "\n")
	fd.Sync()

}

func readAppEventLog(name string) []string {
	path := filepath.Join(defAppsExtFolder, name)
	fn := filepath.Join(path, defAppEventFile)
	fl, err := os.Open(fn)
	if err != nil {
		log.Println("readAppEventLog open:", err)
		return nil
	}

	defer fl.Close()

	var strArray []string
	buf := bufio.NewReader(fl)
	for {
		line, err := buf.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}
		line = strings.TrimSpace(line)
		strArray = append(strArray, line)
	}

	arrLen := len(strArray)
	retPos := 0
	if arrLen > 100 {
		retPos = arrLen - 100
	} else {
		retPos = 0
	}
	return strArray[retPos:]
}

func getAppEventLogFile(name string) string {
	path := filepath.Join(defAppsExtFolder, name)
	fn := filepath.Join(path, defAppEventFile)
	return fn
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

	gCPUThreshold = lst.CPUThreshold
	gMemThreshold = lst.MemThreshold
	gTaskList = append(gTaskList, lst.Items...)
	for k, v := range gTaskList {
		_ = k
		gTaskList[k].Pid = 0
		if v.Enable == 1 {
			gTaskList[k].Cmd = int(APP_CMD_START)
		} else {
			gTaskList[k].Cmd = int(APP_CMD_STOP)
		}

		path := filepath.Join(defAppsExtFolder, gTaskList[k].Name)
		gTaskList[k].cfg = loadAppCfg(path)
	}

	log.Printf("loadAppList: CPUThreshold=%d, MemThreshold=%d\n", gCPUThreshold, gMemThreshold)
}

func loadAppCfg(path string) appCfg {
	cfg := appCfg{}
	fn := filepath.Join(path, defAppCfgFile)
	fl, err := os.Open(fn)
	if err != nil {
		log.Printf("loadAppCfg %s open:%s\n", fn, err.Error())
		return cfg
	}

	defer fl.Close()
	content, err := ioutil.ReadAll(fl)
	if err != nil {
		log.Printf("loadAppCfg %s readall:%s\n", fn, err.Error())
		return cfg
	}

	err = json.Unmarshal(content, &cfg)
	if err != nil {
		log.Printf("loadAppCfg load %s config:%s\n", fn, err.Error())
		return cfg
	}
	return cfg
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

	return execDir
}

func handleTask() {
	gWaitTime = 5 * 60 * time.Second
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

					case APP_CTL_CONFIG_CPU_THRESHOLD:
						handleAppConfigCPUThreshold(ctlReq)

					case APP_CTL_CONFIG_MEM_THRESHOLD:
						handleAppConfigMemThreshold(ctlReq)

					case APP_CTL_QUERY_CPU_THRESHOLD:
						handleAppQueryCPUThreshold(ctlReq)

					case APP_CTL_QUERY_MEM_THRESHOLD:
						handleAppQueryMemThreshold(ctlReq)

					case APP_CTL_CONFIG_CPU_LIMIT:
						handleAppConfigCPULimit(ctlReq)

					case APP_CTL_CONFIG_MEM_LIMIT:
						handleAppConfigMemLimit(ctlReq)

					case APP_CTL_QUERY_CPU_LIMIT:
						handleAppQueryCPULimit(ctlReq)

					case APP_CTL_QUERY_MEM_LIMIT:
						handleAppQueryMemLimit(ctlReq)

					case APP_CTL_LOGS:
						handleAppLogs(ctlReq)

					case APP_CTL_QUERY_ALL_RESOURCE:
						handleAppQueryAllResource(ctlReq)
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
	for k, v := range gTaskList {
		_ = k
		v.Param = ""
		//log.Println(v.Path, ",", v.Param, ",", v.Pid)
		if isAlive(v.Pid) {
			//ret, err := os.Readlink("/proc/" + strconv.Itoa(v.Pid) + "/comm")
			//if err == nil && ret == v.Path {
			cpuRate := getAppCPUPercent(v.Name, v.Pid)
			memRate := getAppMemPercent(v.Name, v.Pid)
			gTaskList[k].CPURate = cpuRate
			gTaskList[k].MemRate = memRate

			if cpuRate > v.CPUThreshold {
				restartApp(k)
				sendWarnNotify(v.Name, "cpu", cpuRate, v.CPUThreshold)
				writeAppEventLog(&gTaskList[k], "restart %s cpu usage rate: %d over threshold %d restart.", v.Name, cpuRate, v.CPUThreshold)
				log.Printf("%s(%d) cpu usage rate: %d over threshold %d restart\n", v.Name, v.Pid, cpuRate, v.CPUThreshold)

				continue
			}

			if memRate > v.MemThreshold {
				restartApp(k)
				sendWarnNotify(v.Name, "mem", memRate, v.MemThreshold)
				writeAppEventLog(&gTaskList[k], "restart %s mem usage rate: %d over threshold %d restart.", v.Name, memRate, v.MemThreshold)
				log.Printf("%s(%d) mem usage rate: %d over threshold %d restart\n", v.Name, v.Pid, memRate, v.MemThreshold)

				continue
			}

			gTaskList[k].Status = int(APP_STATUS_RUNNING)
			continue
			//} else {
			//	log.Println("checkApps 0x0001:", err, ",", v.Path)
			//}
		} else {
			if v.Status == int(APP_STATUS_RUNNING) {
				gTaskList[k].Status = int(APP_STATUS_STOP)
				gTaskList[k].CPURate = 0
				gTaskList[k].MemRate = 0
			}
		}

		if v.Cmd == int(APP_CMD_START) {
			err := startApp(&gTaskList[k])
			if err != nil {
				log.Printf("checkApps %s:%s", v.Path, err.Error())
			}
		}

	}

	endTime := time.Now().UTC()
	var durationTrace = endTime.Sub(gTraceTime)
	if durationTrace > gWaitTime {
		log.Println("checkApps ok: app list count =", len(gTaskList))
		gTraceTime = time.Now().UTC()
	}
}

func restartApp(idx int) error {
	gTaskList[idx].LogEndTime = time.Now().Unix()
	err := syscall.Kill(gTaskList[idx].Pid, 9)
	if err != nil {
		log.Printf("restartApp kill process: %s, pid:%d error: %s\n", gTaskList[idx].Name, gTaskList[idx].Pid, err.Error())
		return err
	}

	cmd := exec.Command(gTaskList[idx].Path, gTaskList[idx].Param)
	if cmd != nil {
		cmd.Dir = filepath.Join(defAppsExtFolder, gTaskList[idx].Name+"/bin")
		libPath := filepath.Join(defAppsExtFolder, gTaskList[idx].Name+"/lib")
		libEnv := fmt.Sprintf("LD_LIBRARY_PATH=/lib:/usr/lib:/home/zxlib:%s", libPath)
		cmd.Env = append(os.Environ(), libEnv)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Start()
		if err != nil {
			log.Println("restartApp 0x0002:", err)
			return err
		}

		go cmd.Wait()
		gTaskList[idx].Pid = cmd.Process.Pid
		gTaskList[idx].Status = int(APP_STATUS_RUNNING)
		gTaskList[idx].StartTime = time.Now().Unix()
		gTaskList[idx].LogEndTime = time.Now().Unix()
		log.Println("restartApp start name =", gTaskList[idx].Name, ", path =", gTaskList[idx].Path, ", pid =", gTaskList[idx].Pid)

		writeAppInfoFile()
		return nil
	}

	log.Println("restartApp exec cmd nil:", gTaskList[idx].Path)
	return errors.New("restartApp exec cmd nil")
}

func startApp(item *taskItem) error {
	cmd := exec.Command(item.Path, item.Param)
	if cmd != nil {
		cmd.Dir = filepath.Join(defAppsExtFolder, item.Name+"/bin")
		libPath := filepath.Join(defAppsExtFolder, item.Name+"/lib")
		libEnv := fmt.Sprintf("LD_LIBRARY_PATH=/lib:/usr/lib:/home/zxlib:%s", libPath)
		cmd.Env = append(os.Environ(), libEnv)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Start()
		if err != nil {
			log.Printf("startApp: app=%s, %s\n", item.Path, err.Error())
			return err
		}

		go cmd.Wait()
		item.Pid = cmd.Process.Pid
		item.Status = int(APP_STATUS_RUNNING)
		item.StartTime = time.Now().Unix()
		item.LogEndTime = time.Now().Unix()
		writeAppInfoFile()

		log.Println("startApp name =", item.Name, ", path =", item.Path, ", pid =", item.Pid)

		return nil
	}

	return errors.New("startApp cmd nil")

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
		writeCtlSimpleRsp(ctl, 1, "Error: File "+ctl.req.Name+" not exist.")
		return
	}
	cmd := exec.Command("/bin/sh", "-c", "tar -zxvf "+fn+" -C "+defAppsExtFolder)
	out, err := cmd.CombinedOutput()
	_ = out
	if err != nil {
		log.Printf("handleAppInstall: %s(%s)\n", string(out), err)
		writeCtlSimpleRsp(ctl, 1, "Install decompress failed.")
		return
	}

	appName := strings.TrimSuffix(ctl.req.Name, ".tar")
	path := filepath.Join(defAppsExtFolder, appName)
	cfg := loadAppCfg(path)
	if false == rsaSignVerify(appName, cfg.BinName) {
		path := filepath.Join(defAppsExtFolder, appName)
		err := os.RemoveAll(path)
		if err != nil {
			log.Println("remove error: ", path, "err: ")
		}
		log.Printf("handleAppInstall: Verify sign failed.\n")
		writeCtlSimpleRsp(ctl, 1, "Verify file sign failed.")
		return
	}
	var item *taskItem
	item = findAppItem(appName)
	if item == nil {
		item = &taskItem{}
		item.Name = appName
		item.Pid = 0
		path := filepath.Join(defAppsExtFolder, appName)
		item.cfg = cfg
		item.Path = filepath.Join(path, "bin/"+item.cfg.BinName)
		item.Cmd = int(APP_CMD_STOP)
		item.Enable = 1
		item.Status = int(APP_STATUS_INSTALL)
		item.CPUThreshold = defCPUThreshold
		item.MemThreshold = defMemThreshold
		item.CPULimit = defCPULimit
		item.MemLimit = defMemLimit
		item.LogStartTime = time.Now().Unix()
		item.LogEndTime = time.Now().Unix()
		item.Version = getAppVersion(appName)
		item.Hash = getAppHash(appName, cfg.BinName)

		writeAppEventLog(item, "install %s success.", ctl.req.Name)
		gTaskList = append(gTaskList, *item)

		writeAppInfoFile()
	}
	writeCtlSimpleRsp(ctl, 0, "Success.")

}

func handleAppStart(ctl *taskCmd) {
	log.Println("handleAppStart")

	item := findAppItem(ctl.req.Name)
	if item != nil {
		path := filepath.Join(defAppsExtFolder, ctl.req.Name+"/bin")
		fn := filepath.Join(path, item.cfg.BinName)
		if false == checkFileIsExist(fn) {
			writeCtlSimpleRsp(ctl, 1, "Error: File "+ctl.req.Name+" not exist.")
			return
		}

		if false == rsaSignVerify(ctl.req.Name, item.cfg.BinName) {
			log.Printf("handleAppStart: Verify sign failed.\n")
			writeCtlSimpleRsp(ctl, 1, "Verify file sign failed.")
			return
		}

		item.Cmd = int(APP_CMD_START)
		if false == isAlive(item.Pid) {
			err := startApp(item)
			if err != nil {
				writeCtlSimpleRsp(ctl, 1, "Operation failed.")
				writeAppEventLog(item, "start %s operation failed.", item.Name)
			} else {
				writeCtlSimpleRsp(ctl, 0, "Success.")
				writeAppEventLog(item, "start %s success.", item.Name)
			}
		} else {
			writeCtlSimpleRsp(ctl, 0, "Success.")
		}
	} else {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		log.Println("handleAppStart findAppItem nil")
	}
}

func handleAppStop(ctl *taskCmd) {
	log.Println("handleAppStop")

	item := findAppItem(ctl.req.Name)
	if item != nil {
		item.Cmd = int(APP_CMD_STOP)
		item.LogEndTime = time.Now().Unix()
		err := syscall.Kill(item.Pid, syscall.SIGKILL)
		log.Printf("handleAppStop: app=%s, pid=%d\n", item.Name, item.Pid)
		if err != nil {
			writeCtlSimpleRsp(ctl, 1, "Operation failed.")
			writeAppEventLog(item, "stop %s operation failed.", item.Name)
		} else {
			item.Pid = 0
			item.Status = int(APP_STATUS_STOP)
			writeAppInfoFile()
			writeCtlSimpleRsp(ctl, 0, "Success.")
			writeAppEventLog(item, "stop %s success.", item.Name)
		}

	} else {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		log.Println("handleAppStop findAppItem nil")
	}
}

func handleAppEnable(ctl *taskCmd) {
	log.Println("handleAppEnable")

	var item *taskItem
	item = findAppItem(ctl.req.Name)
	if item != nil {
		item.Enable = 1
		item.LogEndTime = time.Now().Unix()
		writeAppInfoFile()
		writeCtlSimpleRsp(ctl, 0, "Success.")
		writeAppEventLog(item, "enable %s success.", item.Name)
	} else {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		log.Println("handleAppEnable findAppItem nil")
	}

	/*
		oldMask := syscall.Umask(0)
		err := os.Chmod(fn, 0755)
		syscall.Umask(oldMask)
		if err != nil {
			writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		} else {
			writeCtlSimpleRsp(ctl, 0, "Success.")
		}
	*/
}

func handleAppDisable(ctl *taskCmd) {
	log.Println("handleAppDisable")

	var item *taskItem
	item = findAppItem(ctl.req.Name)
	if item != nil {
		item.Enable = 0
		item.LogEndTime = time.Now().Unix()
		writeAppInfoFile()
		writeCtlSimpleRsp(ctl, 0, "Success.")
		writeAppEventLog(item, "disable %s success.", item.Name)
	} else {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		log.Println("handleAppDisable findAppItem nil")
	}

	/*
		oldMask := syscall.Umask(0)
		err := os.Chmod(fn, 0644)
		syscall.Umask(oldMask)
		if err != nil {
			writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		} else {
			writeCtlSimpleRsp(ctl, 0, "Success.")
		}
	*/
}

func handleAppRM(ctl *taskCmd) {
	path := filepath.Join(defAppsExtFolder, ctl.req.Name)
	fn := filepath.Join(path, ctl.req.Name)
	log.Println("handleAppRM: ", fn)

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
		writeAppEventLog(item, "uninstall %s operation failed.", item.Name)
	} else {
		code = 0
		ret = "Success."
		writeAppEventLog(item, "uninstall %s success.", item.Name)
	}

	removeItem(item)
	writeAppInfoFile()
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
		rsp := &appCtlCmdRsp{}
		rsp.Cmd = ctl.req.Cmd
		rsp.Name = ctl.req.Name
		rsp.Code = 0
		rsp.Result = "Finish."
		rsp.Total = int32(len(appMap))

		appitem := appItem{}
		appitem.Index = 0
		appitem.Name = ctl.req.Name

		var srvList []srvItem
		itemList := findAppList(ctl.req.Name)
		if len(itemList) < 1 {
			writeCtlSimpleRsp(ctl, 2, "File is not exist.")
			return
		}

		for k, v := range itemList {
			_ = k
			item := srvItem{}
			item.Index = int32(k)
			item.Name = "srv" + strconv.Itoa(k)
			item.Enable = int8(v.Enable)
			item.Status = int8(v.Status)
			item.CPUThreshold = v.CPUThreshold
			item.CPULimit = v.CPULimit
			item.CPUUsage = v.CPURate
			item.MemThreshold = v.MemThreshold
			item.MemLimit = v.MemLimit
			item.MemUsage = v.MemRate
			item.StartTime = v.StartTime
			item.LogsStartTime = 0
			item.LogsEndTime = 0
			if ctl.req.Log == 1 {
				appitem.LogFile = getAppEventLogFile(v.Name)
			}

			srvList = append(srvList, item)
			appitem.Version = v.Version
			appitem.Hash = v.Hash
		}

		appitem.SrvTotal = int32(len(srvList))
		appitem.SrvItems = srvList

		rsp.Items = append(rsp.Items, appitem)
		writeCtlRsp(rsp, ctl.remote)
		return
	}

	rsp := &appCtlCmdRsp{}
	rsp.Cmd = ctl.req.Cmd
	rsp.Name = ctl.req.Name
	rsp.Code = 1
	rsp.Result = "Success."
	rsp.Total = int32(len(appMap))

	idx := int32(0)
	for mK, mV := range appMap {
		_ = mK
		appitem := appItem{}
		appitem.Index = idx
		appitem.Name = mK

		var srvList []srvItem
		for k, v := range mV {
			_ = k
			item := srvItem{}
			item.Index = int32(k)
			item.Name = "srv" + strconv.Itoa(k)
			item.Enable = int8(v.Enable)
			item.Status = int8(v.Status)
			item.CPUThreshold = v.CPUThreshold
			item.CPULimit = v.CPULimit
			item.CPUUsage = v.CPURate
			item.MemThreshold = v.MemThreshold
			item.MemLimit = v.MemLimit
			item.MemUsage = v.MemRate
			item.StartTime = v.StartTime
			if ctl.req.Log == 1 {
				item.LogsStartTime = v.LogStartTime
				item.LogsEndTime = v.LogEndTime
			} else {
				item.LogsStartTime = 0
				item.LogsEndTime = 0
			}

			srvList = append(srvList, item)
			appitem.Version = v.Version
			appitem.Hash = v.Hash
		}

		appitem.SrvTotal = int32(len(srvList))
		appitem.SrvItems = srvList

		rsp.Items = append(rsp.Items, appitem)
		idx = idx + 1

		if idx%10 == 0 {
			writeCtlRsp(rsp, ctl.remote)
			rsp.Items = rsp.Items[0:0]
		}
	}

	rsp.Code = 0
	rsp.Result = "Finish."
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

func handleAppConfigCPUThreshold(ctl *taskCmd) {
	log.Printf("handleAppConfigCPUThreshold: %s -> %d\n", ctl.req.Name, ctl.req.Value)
	var item *taskItem
	item = findAppItem(ctl.req.Name)
	if item != nil {
		item.CPUThreshold = ctl.req.Value
		item.LogEndTime = time.Now().Unix()
		writeAppInfoFile()
		writeCtlSimpleRsp(ctl, 0, "Success.")
		writeAppEventLog(item, "config %s cpu threshold success.", item.Name)
	} else {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		log.Println("handleAppConfigCPUThreshold findAppItem nil")
	}
}

func handleAppConfigMemThreshold(ctl *taskCmd) {
	log.Printf("handleAppConfigMemThreshold: %s -> %d\n", ctl.req.Name, ctl.req.Value)
	var item *taskItem
	item = findAppItem(ctl.req.Name)
	if item != nil {
		item.MemThreshold = ctl.req.Value
		item.LogEndTime = time.Now().Unix()
		writeAppInfoFile()
		writeCtlSimpleRsp(ctl, 0, "Success.")
		writeAppEventLog(item, "config %s memory threshold success.", item.Name)
	} else {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		log.Println("handleAppConfigMemThreshold findAppItem nil")
	}
}

func handleAppQueryCPUThreshold(ctl *taskCmd) {
	var item *taskItem
	item = findAppItem(ctl.req.Name)
	if item != nil {
		ret := strconv.Itoa(item.CPUThreshold)
		writeCtlSimpleRsp(ctl, 0, ret)
		log.Printf("handleAppQueryCPUThreshold: %s -> %s", ctl.req.Name, ret)
	} else {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		log.Println("handleAppQueryCPUThreshold findAppItem nil")
	}
}

func handleAppQueryMemThreshold(ctl *taskCmd) {
	var item *taskItem
	item = findAppItem(ctl.req.Name)
	if item != nil {
		ret := strconv.Itoa(item.MemThreshold)
		writeCtlSimpleRsp(ctl, 0, ret)
		log.Printf("handleAppQueryMemThreshold: %s -> %s", ctl.req.Name, ret)
	} else {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		log.Println("handleAppQueryMemThreshold findAppItem nil")
	}
}

func handleAppConfigCPULimit(ctl *taskCmd) {
	log.Printf("handleAppConfigCPULimit: %s -> %d\n", ctl.req.Name, ctl.req.Value)
	var item *taskItem
	item = findAppItem(ctl.req.Name)
	if item != nil {
		item.CPULimit = ctl.req.Value
		item.LogEndTime = time.Now().Unix()
		writeAppInfoFile()
		writeCtlSimpleRsp(ctl, 0, "Success.")
		writeAppEventLog(item, "config %s cpu limit success.", item.Name)
	} else {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		log.Println("handleAppConfigCPULimit findAppItem nil")
	}
}

func handleAppConfigMemLimit(ctl *taskCmd) {
	log.Printf("handleAppConfigMemLimit: %s -> %d\n", ctl.req.Name, ctl.req.Value)
	var item *taskItem
	item = findAppItem(ctl.req.Name)
	if item != nil {
		item.MemLimit = ctl.req.Value
		item.LogEndTime = time.Now().Unix()
		writeAppInfoFile()
		writeCtlSimpleRsp(ctl, 0, "Success.")
		writeAppEventLog(item, "config %s memory limit success.", item.Name)
	} else {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		log.Println("handleAppConfigMemLimit findAppItem nil")
	}
}

func handleAppQueryCPULimit(ctl *taskCmd) {
	var item *taskItem
	item = findAppItem(ctl.req.Name)
	if item != nil {
		ret := strconv.Itoa(item.CPULimit)
		writeCtlSimpleRsp(ctl, 0, ret)
		log.Printf("handleAppQueryCPULimit: %s -> %s", ctl.req.Name, ret)
	} else {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		log.Println("handleAppQueryCPULimit findAppItem nil")
	}
}

func handleAppQueryMemLimit(ctl *taskCmd) {
	var item *taskItem
	item = findAppItem(ctl.req.Name)
	if item != nil {
		ret := strconv.Itoa(item.MemLimit)
		writeCtlSimpleRsp(ctl, 0, ret)
		log.Printf("handleAppQueryMemLimit: %s -> %s", ctl.req.Name, ret)
	} else {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		log.Println("handleAppQueryMemLimit findAppItem nil")
	}
}

func handleAppLogs(ctl *taskCmd) {
	log.Println("handleAppLogs:")

	var ret string
	for k, v := range gTaskList {
		_ = k
		ret += fmt.Sprintf("%s\n", v.LogFile)
	}

	writeCtlSimpleRsp(ctl, 0, ret)
}

func handleAppQueryAllResource(ctl *taskCmd) {
	log.Println("handleAppQueryAllResource:")

	lst := appResourceList{}
	for k, v := range gTaskList {
		_ = k
		res := appResource{}
		res.Name = v.Name
		res.CPUThreshold = v.CPUThreshold
		res.MemThreshold = v.MemThreshold
		lst.Items = append(lst.Items, res)
	}

	data, err := json.Marshal(&lst)
	if err != nil {
		writeCtlSimpleRsp(ctl, 1, "Operation failed.")
		log.Println("handleAppQueryAllResource marshal error:", err)
		return
	}
	writeCtlSimpleRsp(ctl, 0, string(data))
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

	l, err := gUnixConn.WriteToUnix(buf.Bytes(), remote)
	log.Printf("buf size=%d, send size=%d\n", buf.Len(), l)
	if err != nil {
		log.Println("writeCtlRsp error: ", err)
		return
	}
}

func removeItem(item *taskItem) bool {
	if item == nil {
		return false
	}
	for i := 0; i < len(gTaskList); i++ {
		if gTaskList[i].Name == item.Name {
			gTaskList = append(gTaskList[:i], gTaskList[i+1:]...)
			return true
		}
	}
	return false
}

func getAppCPUPercent(name string, pid int) int {
	if pid < 1 {
		return 0
	}
	cmd := fmt.Sprintf("top -b -n1 | grep %s | grep -v grep | grep %d | awk '{print$8}'", name, pid)
	//log.Println("getAppCPUPercent: cmd=", cmd)
	ret := execBashCmd(cmd)
	//log.Println("getAppCPUPercent ret=", ret)
	f, err := strconv.ParseFloat(strings.TrimRight(ret, "%"), 32)
	if err != nil {
		return 0
	}
	//log.Println("getAppCPUPercent: ", int(f))
	return int(f)
}

func getAppMemPercent(name string, pid int) int {
	if pid < 1 {
		return 0
	}
	cmd := fmt.Sprintf("top -b -n1 | grep %s | grep -v grep | grep %d | awk '{print$6}'", name, pid)
	//log.Println("getAppMemPercent: cmd=", cmd)
	ret := execBashCmd(cmd)
	//log.Println("getAppMemPercent ret=", ret)
	f, err := strconv.ParseFloat(strings.TrimRight(ret, "%"), 32)
	if err != nil {
		return 0
	}
	//log.Println("getAppMemPercent: ", int(f))
	return int(f)
}

func getAppMem(name string, pid int) int {
	if pid < 1 {
		return 0
	}
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

func getAppHash(appName, binName string) string {
	path := filepath.Join(defAppsExtFolder, appName+"/bin")
	fn := filepath.Join(path, binName)
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

func getContainerID() string {
	fl, err := os.Open("/etc/hostname")
	if err != nil {
		log.Println("getContainerID 0x0001:", err)
		return ""
	}

	defer fl.Close()
	content, err := ioutil.ReadAll(fl)
	if err != nil {
		log.Println("getContainerID 0x0002:", err)
		return ""
	}

	return formatString(string(content))
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

func getAppHashBytes(appName, binName string) []byte {
	fn := filepath.Join(defAppsExtFolder, appName+"/bin/"+binName)
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

func getAppSign(appName, binName string) []byte {
	path := filepath.Join(defAppsExtFolder, appName)
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

func rsaSignVerify(appName, binName string) bool {
	data := getAppHashBytes(appName, binName)
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

	signature := getAppSign(appName, binName)
	if signature == nil {
		return false
	}
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

func writeAppInfoFile() {
	lst := taskList{}
	lst.Items = append(lst.Items, gTaskList...)
	writeFile(&lst)
}

func sendWarnNotify(name, kind string, value, threshold int) {
	warn := warnNotify{}
	warn.Cid = gContainerID
	warn.Kind = kind
	warn.Name = name
	warn.Threshold = threshold
	warn.Value = value
	data, err := json.Marshal(&warn)
	if err != nil {
		log.Println("sendWarnNotify marshal error:", err)
		return
	}

	_, err = gUDPConn.Write(data)
	if err != nil {
		log.Println("sendWarnNotify udp write error:", err)
		return
	}
}
