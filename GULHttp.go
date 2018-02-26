package main

import (
	"net"
	"log"
	"net/http"
	"rlrs"
	"github.com/golang/protobuf/proto"
	"github.com/larspensjo/config"
	"fmt"
	_"html"
	_"time"
	"runtime"
	"flag"
	"strconv"
	_"strings"
	"time"
	"sync"
	"encoding/json"
	"sync/atomic"
	"netframe"
	"os"
	"io"
	_"net/http/pprof"
	_"runtime/pprof"
)

type udpData struct {
	data []byte
}

type UserItem struct {
	UserID int64
	EnterTime int64
}

type UserList struct {
	RoomID int64
	UserArray []*UserItem
}

var (
	g_http_conc_num int64
	g_total_req_num int64
	g_total_rsp_num int64
	g_total_timeout_num int64
	g_total_udp_req_num int64
	g_total_udp_rsp_num int64
	g_total_send_chan_num int64
	g_total_recv_chan_num int64
	g_total_lost_chan_num int64
	g_session_id uint64
	g_chan_map [100]sync.Map

	g_log *log.Logger
	g_udp_conn *net.UDPConn
	g_udp_addr *net.UDPAddr
	g_white_list []int
	g_config_file = flag.String("configfile", "config.ini", "configfile")
	g_byte_pool = sync.Pool{New: func() interface{} {
		b := make([]byte, 2048)
		return b
	}}
	/*
	g_chan_data_pool = sync.Pool{New: func() interface{} {
		chanData := make(chan *udpData, 10)
		return chanData
	}}*/
)

func init() {
	_ = g_byte_pool
}

func main() {
	g_log = log.New(os.Stdout, "\r\n", log.LstdFlags | log.Lshortfile)
	//log.SetFlags(log.LstdFlags | log.Lshortfile)
	//log.SetPrefix("\r\n")
	strPath, err := os.Getwd()
	file, err := os.Create(strPath+"\\log\\http.log")
	if err != nil {
		log.Println("create log file error: ", err)
	} else {
		defer file.Close()

		writers := []io.Writer {
			file,
			os.Stdout,
		}

		g_log.SetOutput(io.MultiWriter(writers...))
	}

	runtime.GOMAXPROCS(runtime.NumCPU())
	flag.Parse()
	cfg, err := config.ReadDefault(*g_config_file)
	if err != nil {
		g_log.Fatalln(err)
	}

	httpIP, httpPort := parseIpPort("http", cfg)
	g_log.Println("http: ", httpIP, ":", httpPort)

	rlrsIP, rlrsPort := parseIpPort("rlrs", cfg)
	g_log.Println("rlrs: ", rlrsIP, ":", rlrsPort)

	loadWhiteList(cfg)
	g_log.Println(g_white_list)

	for i := 0; i < 100; i++ {
		g_chan_map[i] = sync.Map{}
	}

	port := int(rlrsPort)
	g_udp_addr, err = net.ResolveUDPAddr("udp", rlrsIP+":"+strconv.Itoa(port))
	if err != nil {
		g_log.Fatalln(err)
	}
	udpAddr, err := net.ResolveUDPAddr("udp", ":9000")
	if err != nil {
		g_log.Fatalln(err)
	}
	g_udp_conn, err = net.ListenUDP("udp", udpAddr)
	if err != nil {
		g_log.Fatalln(err)
	}

	defer g_udp_conn.Close()
	go handleUDPClient(g_udp_conn)

	g_log.Println("start server...")

	go func() {
		var lastReqNum, lastRspNum  int64
		var lastTimeoutNum int64
		var lastUdpReqNum, lastUdpRspNum int64
		for {
			reqNum := atomic.LoadInt64(&g_total_req_num)
			rspNum := atomic.LoadInt64(&g_total_rsp_num)
			timeoutNum := atomic.LoadInt64(&g_total_timeout_num)
			udpReqNum := atomic.LoadInt64(&g_total_udp_req_num)
			udpRspNum := atomic.LoadInt64(&g_total_udp_rsp_num)

			req := (reqNum - lastReqNum) / 5
			rsp := (rspNum - lastRspNum) / 5
			tmoNum := (timeoutNum - lastTimeoutNum) / 5
			udpReq := (udpReqNum - lastUdpReqNum) / 5
			udpRsp := (udpRspNum - lastUdpRspNum) / 5

			g_log.Println("当前HTTP并发数: ", atomic.LoadInt64(&g_http_conc_num), ", 当前goroutine数: ", runtime.NumGoroutine())
			g_log.Println("HTTP请求总数: ", reqNum, ", 应答总数: ", rspNum, ", 每秒请求: ", req, ", 每秒应答: ", rsp)
			g_log.Println("HTTP超时总数: ", timeoutNum, ", 每秒超时数: ", tmoNum)
			g_log.Println("UDP请求总数: ", udpReqNum, ", 应答总数: ", udpRspNum, ", 每秒请求: ", udpReq, ", 每秒应答: ", udpRsp)
			g_log.Println("Chan发送总数: ", atomic.LoadInt64(&g_total_send_chan_num), ", 接收总数: ", atomic.LoadInt64(&g_total_recv_chan_num))
			g_log.Println("ChanMap not find: ", atomic.LoadInt64(&g_total_lost_chan_num))

			lastReqNum = reqNum
			lastRspNum = rspNum
			lastTimeoutNum = tmoNum
			lastUdpReqNum = udpReqNum
			lastUdpRspNum = udpRspNum
			time.Sleep(time.Second * 5)
		}
	}()

	httpSrv := &http.Server {
		Addr: ":8090",
		ReadTimeout: 90 * time.Second,
		WriteTimeout: 90 * time.Second,
		ErrorLog: g_log,
	}

	http.HandleFunc("/getUserList", userListHandler)
	err = httpSrv.ListenAndServe()
	if err != nil {
		g_log.Println("http server error: ", err)
	}
}

func parseIpPort(sec string, cfg *config.Config) (ip string, port uint16) {
	if cfg.HasSection(sec) {
		_, err := cfg.SectionOptions(sec)
		if err != nil {
			return
		}

		ip, _ = cfg.String(sec, "ip")
		pt, _ := cfg.Int(sec, "port")
		port = uint16(pt)
	}
	return
}

func loadWhiteList(cfg *config.Config) {
	if cfg.HasSection("white") {
		_, err := cfg.SectionOptions("white")
		if err != nil {
			return
		}

		count, err := cfg.Int("white", "count")
		if err != nil {
			return
		}

		for i := 0; i < count; i++ {
			ip, _ := cfg.String("white", "ip_"+strconv.Itoa(i+1))
			netIP := netframe.StringIpToNetInt(ip)
			g_white_list = append(g_white_list, netIP)
		}
	}
}

func userListHandler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		atomic.AddInt64(&g_http_conc_num, -1)
	}()

	startTime := time.Now().UnixNano()
	atomic.AddInt64(&g_http_conc_num, 1)

	strHost, strPort, _ := net.SplitHostPort(r.RemoteAddr)
	_ = strPort
	//g_log.Println("host: ", strHost, " :port: ", strPort)
	netHost := netframe.StringIpToNetInt(strHost)
	var bExist bool = false
	for _, v := range g_white_list {
		if v == netHost {
			bExist = true
			break
		}
	}
	if !bExist {
		w.WriteHeader(401)
		return
	}

	atomic.AddInt64(&g_total_req_num, 1)
	r.ParseForm()
	strRoomId := r.Form.Get("roomid")
	strLastUserID := r.Form.Get("lastuserid")
	strRobot := r.Form.Get("rb")
	//g_log.Println("roomid:", strRoomId, ", userid:", strLastUserID, " :rb: ", strRobot)
	rid, err := strconv.ParseInt(strRoomId, 10, 64)
	uid, err := strconv.ParseInt(strLastUserID, 10, 64)
	rb, err := strconv.Atoi(strRobot)

	atomic.AddUint64(&g_session_id, 1)
	sid := atomic.LoadUint64(&g_session_id)
	udpChan := make(chan *udpData, 1000)
	g_chan_map[sid%100].Store(sid, udpChan)

	defer func() {
		g_chan_map[sid%100].Delete(sid)
		//g_log.Println("delete sid = ", sid)
	}()

	cmd := uint32(qiqi_rlrs.ENUM_RLRS_CMD_enum_rlrs_get_room_user_list)
	int32Rb := int32(rb)

	getUL := func (lastUid int64) {
		reqUserList := &qiqi_rlrs.ReqGetRoomUserList{}
		reqUserList.Uint32Cmd = &cmd
		reqUserList.Uint64Jobid = &sid
		reqUserList.Int64Roomid = &rid
		reqUserList.Int64LastUserid = &lastUid
		reqUserList.Int32Robot = &int32Rb
		reqBuf, err := proto.Marshal(reqUserList)
		if err != nil {
			g_log.Println("pb pack error: ", err)
			return
		}

		n, err := g_udp_conn.WriteToUDP(reqBuf, g_udp_addr)
		_ = n
		//g_log.Println("write udp data: ", n, ", err:", err, "addr:", g_udp_addr)
		if err != nil {
			log.Println("write udp data error: ", err)
		} else {
			atomic.AddInt64(&g_total_udp_req_num, 1)
		}
	}

	getUL(uid)

	var userArray []*UserItem
	LoopFor:
	for {
		select {
			case uData, ok := <- udpChan: {
				if !ok {
					g_log.Println("chan close: sid = ", sid)
					break LoopFor
				}

				atomic.AddInt64(&g_total_recv_chan_num, 1)
				rspUserList := &qiqi_rlrs.RspGetRoomUserList{}
				err = proto.Unmarshal(uData.data, rspUserList)
				if err != nil {
					g_log.Println("http pb parse failed: sid = ", sid, ", error: ", err)
					w.WriteHeader(500)

					break LoopFor
				}

				//存储数据
				ulData := rspUserList.GetOUserList()
				if len(ulData) > 0 {
					for i := 0; i < len(ulData); i++ {
						userItem := &UserItem{ulData[i].GetInt64Userid(), ulData[i].GetInt64EnterTime()}
						userArray = append(userArray, userItem)
					}
				}

				//结束
				if 0 == rspUserList.GetInt64LastUserid() {
					atomic.AddInt64(&g_total_rsp_num, 1)
					userList := UserList{rspUserList.GetInt64Roomid(), userArray}
					js, err := json.Marshal(userList)
					if err != nil {
						w.WriteHeader(500)
						g_log.Println("http json err: ", err)
					} else {
						fmt.Fprintf(w, "%v", string(js))
					}

					break LoopFor
				}

				//取下一组
				uid = rspUserList.GetInt64LastUserid()
				getUL(uid)
			}

			case <- time.After(time.Second * 60): {
				atomic.AddInt64(&g_total_timeout_num, 1)
				g_log.Println("http timeout: sid = ", sid)
				w.WriteHeader(500)

				break LoopFor
			}
		}
	}

	useTime := (time.Now().UnixNano() - startTime) / int64(time.Millisecond)
	_ = useTime
	//g_log.Println("http end sid = ", sid, ", use time:", useTime)
}

func handleUDPClient(conn *net.UDPConn) {
	defer conn.Close()

	data := make([]byte, 2048)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(data)
		_ = remoteAddr
		if err != nil {
			g_log.Println("udp recv error: ", err)
			break
		}
		if n < 1 {
			continue
		}
		if n > 2048 {
			g_log.Println("udp recv size: ", n)
		}

		atomic.AddInt64(&g_total_udp_rsp_num, 1)
		//g_log.Println("recv udp data: ", n, " addr:", remoteAddr)

		rspUserList := &qiqi_rlrs.RspGetRoomUserList{}
		err = proto.Unmarshal(data[:n], rspUserList)
		if err != nil {
			g_log.Println("pb parse error: ", err, ", recv udp data size: ", n)
			continue
		}

		sid := rspUserList.GetUint64Jobid()
		udpChan, ok := g_chan_map[sid%100].Load(sid)
		//g_log.Println("chan:", udpChan, " : ", ok)
		if !ok {
			g_log.Println("chan is not exist: sid = ", sid)
			atomic.AddInt64(&g_total_lost_chan_num, 1)
			continue
		}

		uc, ok := udpChan.(chan *udpData)
		//g_log.Println("chan22:", uc, " : ", ok)
		if !ok {
			g_log.Println("chan data ref failed: sid = ", sid)
			continue
		}

		udpPack := &udpData{}
		udpPack.data = make([]byte, n)
		copy(udpPack.data, data[:n])
		uc <- udpPack
		//g_log.Println("send chan data")
		atomic.AddInt64(&g_total_send_chan_num, 1)

		//结束
		if 0 == rspUserList.GetInt64LastUserid() {
			//close(uc)
			//g_log.Println("roomid:", rspUserList.GetInt64Roomid(), " end")
		}
	}
}
