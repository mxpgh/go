package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const cfgFile string = "monitor.cfg"

var (
	gDstUnixAddr *net.UnixAddr
	gUnixAddr    *net.UnixAddr
	gUnixConn    *net.UnixConn
)

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

func main() {
	log.Println("appctl version1.0.0")
	len := len(os.Args)
	if len < 2 {
		fmt.Println("args less: len=", len)
		return
	}

	gDstUnixAddr, err := net.ResolveUnixAddr("unixgram", "/var/run/appctl-daemon.sock")
	if err != nil {
		log.Println("resolve dst addr error: ", err)
		return
	}
	_ = gDstUnixAddr

	syscall.Unlink("/var/run/appctl-cli.sock")
	gUnixAddr, err := net.ResolveUnixAddr("unixgram", "/var/run/appctl-cli.sock")
	if err != nil {
		log.Println("resolve addr error: ", err)
		return
	}

	gUnixConn, err = net.DialUnix("unixgram", gUnixAddr, gDstUnixAddr)
	if err != nil {
		log.Println("connect error: ", err)
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
		log.Println("Caught signal％s：shutting down。", sig)
		//停止监听（如果unix类型，则取消套接字连接）：
		gUnixConn.Close()
		//os.Remove("/var/run/appctl-cli.sock")
		//我们完成了：
		os.Exit(0)
	}(sig)

	go readUnixgram()

	switch os.Args[1] {
	case "-start":
		{
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_START
			ctl.Name = os.Args[2]
			writeUnixgram(&ctl)
		}
	case "-stop":
		{
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_STOP
			ctl.Name = os.Args[2]
			writeUnixgram(&ctl)
		}
	case "-enable":
		{
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_ENABLE
			ctl.Name = os.Args[2]
			writeUnixgram(&ctl)
		}
	case "-disable":
		{
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_DISABLE
			ctl.Name = os.Args[2]
			writeUnixgram(&ctl)
		}
	case "-rm":
		{
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_RM
			ctl.Name = os.Args[2]
			writeUnixgram(&ctl)
		}
	case "-list":
		{
			ctl := appCtlCmdReq{}
			ctl.Cmd = APP_CTL_RM
			ctl.Name = os.Args[2]
			if os.Args[3] == "-log" {
				ctl.Log = 1
			} else {
				ctl.Log = 0
			}

			writeUnixgram(&ctl)
		}
	default:
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
		gUnixConn.SetReadDeadline(time.Second * 3)
		buf := make([]byte, 1400)
		size, err := gUnixConn.Read(buf)
		if err != nil {
			log.Println("readUnixgram error: ", err)
			break
		}
		fmt.Println("recv:", string(buf[:size]))
	}
	return
}

func writeUnixgram(req *appCtlCmdReq) {
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	err := enc.Encode(req)
	if err != nil {
		log.Println("gob encode error: ", err)
		return
	}
	_, err = gUnixConn.Write(buf.Bytes())
	if err != nil {
		log.Println("writeUnixgram error: ", err)
		return
	}
}
