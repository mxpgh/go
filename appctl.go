package main

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const cfgFile string = "monitor.cfg"

var (
	gDstUnixAddr *net.UnixAddr
	gUnixAddr    *net.UnixAddr
	gUnixConn    *net.UnixConn
	gCtlCmdRsp   appCtlCmdRsp
	gLog         *log.Logger
)

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
	APP_STATUS_INSTALL
	APP_STATUS_RUNNING
	APP_STATUS_STOP
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
	Index   int32
	Name    string
	Version string
	Hash    string

	SrvTotal int32
	SrvItems []srvItem
	LogFile  string
}

func main() {
	//log.Println("appctl version1.0.0")
	l := len(os.Args)
	if l < 2 {
		fmt.Println("args less: len=", l)
		return
	}

	gLog = log.New(os.Stdout, "\r\n", log.LstdFlags|log.Lshortfile)
	file, err := os.Create("/usr/local/monitor/appctl.log")
	if err != nil {
		log.Println("create log file error: ", err)
	} else {
		defer file.Close()

		writers := []io.Writer{
			file,
			//os.Stdout,
		}

		gLog.SetOutput(io.MultiWriter(writers...))
	}

	gDstUnixAddr, err := net.ResolveUnixAddr("unixgram", "/var/run/appctl-daemon.sock")
	if err != nil {
		gLog.Println("resolve dst addr error: ", err)
		return
	}
	_ = gDstUnixAddr

	syscall.Unlink("/var/run/appctl-cli.sock")
	gUnixAddr, err := net.ResolveUnixAddr("unixgram", "/var/run/appctl-cli.sock")
	if err != nil {
		gLog.Println("resolve addr error: ", err)
		return
	}

	gUnixConn, err = net.DialUnix("unixgram", gUnixAddr, gDstUnixAddr)
	if err != nil {
		gLog.Println("connect error: ", err)
		return
	}

	defer func() {
		//gUnixConn.Close()
		//os.Remove("/var/run/appctl-cli.sock")
	}()

	//go check()
	//处理常见的进程终止信号，以便我们可以正常关闭：
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func(c chan os.Signal) {
		//等待SIGINT或SIGKILL：
		sig := <-c
		gLog.Println("Caught signal％s：shutting down。", sig)
		//停止监听（如果unix类型，则取消套接字连接）：
		gUnixConn.Close()
		//os.Remove("/var/run/appctl-cli.sock")
		//我们完成了：
		os.Exit(0)
	}(sig)

	go readUnixgram()

	switch os.Args[1] {
	case "-install":
		{
			if len(os.Args) < 3 {
				fmt.Println("Command args error.")
				os.Exit(0)
				return
			}
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_INSTALL
			ctl.Name = os.Args[2]
			writeUnixgram(&ctl)
		}
	case "-start":
		{
			if len(os.Args) < 3 {
				fmt.Println("Command args error.")
				os.Exit(0)
				return
			}
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_START
			ctl.Name = os.Args[2]
			writeUnixgram(&ctl)
		}
	case "-stop":
		{
			if len(os.Args) < 3 {
				fmt.Println("Command args error.")
				os.Exit(0)
				return
			}
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_STOP
			ctl.Name = os.Args[2]
			writeUnixgram(&ctl)
		}
	case "-enable":
		{
			if len(os.Args) < 3 {
				fmt.Println("Command args error.")
				os.Exit(0)
				return
			}
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_ENABLE
			ctl.Name = os.Args[2]
			writeUnixgram(&ctl)
		}
	case "-disable":
		{
			if len(os.Args) < 3 {
				fmt.Println("Command args error.")
				os.Exit(0)
				return
			}

			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_DISABLE
			ctl.Name = os.Args[2]
			writeUnixgram(&ctl)
		}
	case "-rm":
		{
			if len(os.Args) < 3 {
				fmt.Println("Command args error.")
				os.Exit(0)
				return
			}

			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_RM
			ctl.Name = os.Args[2]
			writeUnixgram(&ctl)
		}
	case "-list":
		{
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_LIST
			if len(os.Args) > 2 {
				if os.Args[2] != "-log" {
					ctl.Name = os.Args[2]
					if len(os.Args) > 3 {
						if os.Args[3] == "-log" {
							ctl.Log = 1
						} else {
							ctl.Log = 0
						}
					} else {
						ctl.Log = 0
					}

				} else {
					ctl.Name = ""
					ctl.Log = 1
				}
			} else {
				ctl.Name = ""
				ctl.Log = 0
			}

			writeUnixgram(&ctl)
		}
	case "-version":
		{
			if len(os.Args) < 3 {
				fmt.Println("Command args error.")
				os.Exit(0)
				return
			}
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_VERSION
			ctl.Name = os.Args[2]
			writeUnixgram(&ctl)
		}
	case "-cpu":
		{
			if len(os.Args) < 4 {
				fmt.Println("Command args error.")
				os.Exit(0)
				return
			}
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_CONFIG_CPU_THRESHOLD
			ctl.Name = os.Args[3]
			val, err := strconv.Atoi(os.Args[2])
			if err != nil {
				fmt.Println("Command args value error.")
				os.Exit(0)
			} else {
				ctl.Value = val
			}
			writeUnixgram(&ctl)
		}
	case "-mem":
		{
			if len(os.Args) < 4 {
				fmt.Println("Command args error.")
				os.Exit(0)
				return
			}
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_CONFIG_MEM_THRESHOLD
			ctl.Name = os.Args[3]
			val, err := strconv.Atoi(os.Args[2])
			if err != nil {
				fmt.Println("Command args value error.")
				os.Exit(0)
			} else {
				ctl.Value = val
			}
			writeUnixgram(&ctl)
		}
	case "-cpulimit":
		{
			if len(os.Args) < 4 {
				fmt.Println("Command args error.")
				os.Exit(0)
				return
			}
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_CONFIG_CPU_LIMIT
			ctl.Name = os.Args[3]
			val, err := strconv.Atoi(os.Args[2])
			if err != nil {
				fmt.Println("Command args value error.")
				os.Exit(0)
			} else {
				ctl.Value = val
			}
			writeUnixgram(&ctl)
		}
	case "-memlimit":
		{
			if len(os.Args) < 4 {
				fmt.Println("Command args error.")
				os.Exit(0)
				return
			}
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_CONFIG_MEM_LIMIT
			ctl.Name = os.Args[3]
			val, err := strconv.Atoi(os.Args[2])
			if err != nil {
				fmt.Println("Command args value error.")
				os.Exit(0)
			} else {
				ctl.Value = val
			}
			writeUnixgram(&ctl)
		}
	case "-query":
		{
			if len(os.Args) < 4 {
				fmt.Println("Command args error.")
				os.Exit(0)
				return
			}
			if os.Args[2] == "cpu" {
				ctl := appCtlCmdReq{}
				ctl.Cmd = APP_CTL_QUERY_CPU_THRESHOLD
				ctl.Name = os.Args[3]
				writeUnixgram(&ctl)
			} else if os.Args[2] == "mem" {
				ctl := appCtlCmdReq{}
				ctl.Cmd = APP_CTL_QUERY_MEM_THRESHOLD
				ctl.Name = os.Args[3]
				writeUnixgram(&ctl)
			} else if os.Args[2] == "cpulimit" {
				ctl := appCtlCmdReq{}
				ctl.Cmd = APP_CTL_QUERY_CPU_LIMIT
				ctl.Name = os.Args[3]
				writeUnixgram(&ctl)
			} else if os.Args[2] == "memlimit" {
				ctl := appCtlCmdReq{}
				ctl.Cmd = APP_CTL_QUERY_MEM_LIMIT
				ctl.Name = os.Args[3]
				writeUnixgram(&ctl)
			} else {
				fmt.Println("Command args error.")
				os.Exit(0)
				return
			}
		}
	case "-queryall":
		{
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_QUERY_ALL_RESOURCE
			writeUnixgram(&ctl)
		}
	case "-logs":
		{
			if len(os.Args) < 3 {
				fmt.Println("Command args error.")
				os.Exit(0)
				return
			}
			if os.Args[2] == "files" {
				ctl := appCtlCmdReq{}
				ctl.Cmd = APP_CTL_LOGS
				writeUnixgram(&ctl)
			} else {
				fmt.Println("Command args error.")
				os.Exit(0)
				return
			}
		}
	default:
		fmt.Println("Command args error.")
		os.Exit(0)
		return
	}

	select {}
}

func closeUinxgram(ext bool) {
	gUnixConn.Close()
	os.Remove("/var/run/appctl-cli.sock")
	if ext {
		os.Exit(0)
	}
}

func readUnixgram() {
	for {
		t := time.Now()
		gUnixConn.SetReadDeadline(t.Add(time.Duration(10 * time.Second)))
		buf := make([]byte, 1024*16)
		size, err := gUnixConn.Read(buf)
		if err != nil {
			fmt.Println("readUnixgram error: ", err)
			break
		}
		data := bytes.NewBuffer(buf[:size])
		dec := gob.NewDecoder(data)
		ctlRsp := appCtlCmdRsp{}
		err = dec.Decode(&ctlRsp)
		if err != nil {
			fmt.Println("decode error: ", err)
			break
		}
		switch ctlRsp.Cmd {
		case APP_CTL_INSTALL:
			if 0 == ctlRsp.Code {

			} else {
				fmt.Println(ctlRsp.Result)
			}

		case APP_CTL_START:
			if 0 == ctlRsp.Code {

			} else {
				fmt.Println(ctlRsp.Result)
			}

		case APP_CTL_STOP:
			if 0 == ctlRsp.Code {

			} else {
				fmt.Println(ctlRsp.Result)
			}

		case APP_CTL_ENABLE:
			if 0 == ctlRsp.Code {

			} else {
				fmt.Println(ctlRsp.Result)
			}

		case APP_CTL_DISABLE:
			if 0 == ctlRsp.Code {

			} else {
				fmt.Println(ctlRsp.Result)
			}

		case APP_CTL_RM:
			if 0 == ctlRsp.Code {

			} else {
				fmt.Println(ctlRsp.Result)
			}

		case APP_CTL_LIST:
			if 2 == ctlRsp.Code {
				fmt.Println(ctlRsp.Result)
			} else {
				ret := handleAppList(&ctlRsp)
				if ret == 1 {
					continue
				}
			}

		case APP_CTL_VERSION:
			if 0 == ctlRsp.Code {
				fmt.Println(ctlRsp.Result)
			} else {
				//log.Println(ctlRsp.Result)
			}

		case APP_CTL_CONFIG_CPU_THRESHOLD:
			if 0 == ctlRsp.Code {
				//fmt.Println(ctlRsp.Result)
			} else {
				log.Println(ctlRsp.Result)
			}

		case APP_CTL_CONFIG_MEM_THRESHOLD:
			if 0 == ctlRsp.Code {
				//fmt.Println(ctlRsp.Result)
			} else {
				log.Println(ctlRsp.Result)
			}

		case APP_CTL_QUERY_CPU_THRESHOLD:
			if 0 == ctlRsp.Code {
				fmt.Println(ctlRsp.Result)
			} else {
				log.Println(ctlRsp.Result)
			}

		case APP_CTL_QUERY_MEM_THRESHOLD:
			if 0 == ctlRsp.Code {
				fmt.Println(ctlRsp.Result)
			} else {
				log.Println(ctlRsp.Result)
			}

		case APP_CTL_CONFIG_CPU_LIMIT:
			if 0 == ctlRsp.Code {
				//fmt.Println(ctlRsp.Result)
			} else {
				log.Println(ctlRsp.Result)
			}

		case APP_CTL_CONFIG_MEM_LIMIT:
			if 0 == ctlRsp.Code {
				//fmt.Println(ctlRsp.Result)
			} else {
				log.Println(ctlRsp.Result)
			}

		case APP_CTL_QUERY_CPU_LIMIT:
			if 0 == ctlRsp.Code {
				fmt.Println(ctlRsp.Result)
			} else {
				log.Println(ctlRsp.Result)
			}

		case APP_CTL_QUERY_MEM_LIMIT:
			if 0 == ctlRsp.Code {
				fmt.Println(ctlRsp.Result)
			} else {
				log.Println(ctlRsp.Result)
			}

		case APP_CTL_QUERY_ALL_RESOURCE:
			if 0 == ctlRsp.Code {
				fmt.Println(ctlRsp.Result)
			} else {
				log.Println(ctlRsp.Result)
			}

		case APP_CTL_LOGS:
			if 0 == ctlRsp.Code {
				fmt.Println(ctlRsp.Result)
			} else {
				log.Println(ctlRsp.Result)
			}
		}
		break
	}

	os.Exit(0)
	return
}

func writeUnixgram(req *appCtlCmdReq) {
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	err := enc.Encode(req)
	if err != nil {
		fmt.Println("gob encode error: ", err)
		os.Exit(0)
		return
	}
	_, err = gUnixConn.Write(buf.Bytes())
	if err != nil {
		fmt.Println("writeUnixgram error: ", err)
		os.Exit(0)
		return
	}
}

func readAppEventLog(fn string) []string {
	fl, err := os.Open(fn)
	if err != nil {
		//log.Println("readAppEventLog open:", err)
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

func handleAppList(rsp *appCtlCmdRsp) int {
	gCtlCmdRsp.Cmd = rsp.Cmd
	gCtlCmdRsp.Name = rsp.Name
	gCtlCmdRsp.Code = rsp.Code
	gCtlCmdRsp.Result = rsp.Result
	gCtlCmdRsp.Total = rsp.Total
	gCtlCmdRsp.Items = append(gCtlCmdRsp.Items, rsp.Items...)

	if rsp.Code != 0 {
		return 1
	}
	gLog.Println("handleAppList recv finish.")
	bHaveEnter := false
	fmt.Printf("Total app number %d \n\n", gCtlCmdRsp.Total)

	for k, v := range gCtlCmdRsp.Items {
		_ = k
		fmt.Printf("%-20s: %d\n", "App index", v.Index)
		fmt.Printf("%-20s: %s\n", "App name", v.Name)
		fmt.Printf("%-20s: %s\n", "App version", v.Version)
		fmt.Printf("%-20s: %s\n", "App hash", v.Hash)

		for i, t := range v.SrvItems {
			_ = i
			if t.Status == int8(APP_STATUS_INSTALL) {
				continue
			}

			fmt.Printf("%-20s: %d\n", "Service index", t.Index)
			fmt.Printf("%-20s: %s\n", "Service name", t.Name)

			if t.Enable == 1 {
				fmt.Printf("%-20s: yes\n", "Service enable")
			} else {
				fmt.Printf("%-20s: no\n", "Service enable")
			}

			if t.Status == int8(APP_STATUS_RUNNING) {
				fmt.Printf("%-20s: running\n", "Service status")
			} else {
				fmt.Printf("%-20s: stop\n", "Service status")
			}

			fmt.Printf("%-20s: %d%%\n", "CPU threshold", t.CPUThreshold)
			fmt.Printf("%-20s: %d%%\n", "CPU usage", t.CPUUsage)
			fmt.Printf("%-20s: %d%%\n", "Mem threshold", t.MemThreshold)
			fmt.Printf("%-20s: %d%%\n", "Mem usage", t.MemUsage)
			fmt.Printf("%-20s: %s\n", "Start time", time.Unix(t.StartTime, 0).Format("2006-01-02 15:04:05"))

			if t.LogsStartTime != 0 {
				fmt.Printf("-- Logs begin at %s, end at %s, --\n", time.Unix(t.LogsStartTime, 0).Format("2006-01-02 15:04:05"), time.Unix(t.LogsEndTime, 0).Format("2006-01-02 15:04:05"))
			}

			fmt.Printf("\n")
			bHaveEnter = true
		}

		if len(v.LogFile) > 0 {
			strLogs := readAppEventLog(v.LogFile)
			for logK, logV := range strLogs {
				_ = logK
				fmt.Printf("%s\n", logV)
				bHaveEnter = true
			}
		}

		if !bHaveEnter {
			fmt.Printf("\n")
		}
	}
	gLog.Println("handleAppList display finish.")
	gCtlCmdRsp.Items = gCtlCmdRsp.Items[0:0]
	return 0
}
