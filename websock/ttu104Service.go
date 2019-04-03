package main

import (
	"bytes"
	"encoding/binary"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/gorilla/websocket"
)

type cmd104 struct {
	wr []byte
	rd []byte
}

type cmdIndex int32

const (
	U104 cmdIndex = iota
	CALL
	TEST
)

var ttu104 = []cmd104{
	{
		[]byte{0x68, 0x04, 0x07, 0x00, 0x00, 0x00},
		[]byte{0x68, 0x04, 0x0B, 0x00, 0x00, 0x00},
	},
	{
		[]byte{0x68, 0x0E, 0x00, 0x00, 0x00, 0x00, 0x64, 0x01, 0x06, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x14},
		[]byte{0x68, 0x0E, 0x00, 0x00, 0x02, 0x00, 0x64, 0x01, 0x07, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x14},
	},
	{
		[]byte{0x68, 0x04, 0x43, 0x00, 0x00, 0x00},
		[]byte{0x68, 0x04, 0x83, 0x00, 0x00, 0x00},
	},
}

/*
var (
	u104        = []byte{0x68, 0x04, 0x07, 0x00, 0x00, 0x00}
	u104ok      = []byte{0x68, 0x04, 0x0B, 0x00, 0x00, 0x00}
	call104     = []byte{0x68, 0x0E, 0x00, 0x00, 0x00, 0x00, 0x64, 0x01, 0x06, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x14}
	call104ok   = []byte{0x68, 0x0E, 0x00, 0x00, 0x02, 0x00, 0x64, 0x01, 0x07, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x14}
	u104test    = []byte{0x68, 0x04, 0x43, 0x00, 0x00, 0x00}
	u104testRsp = []byte{0x68, 0x04, 0x83, 0x00, 0x00, 0x00}
)*/

var templatePath string
var upgrader = &websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}

func getCurrentDirectory() string {
	dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatal(err)
	}
	return strings.Replace(dir, "\\", "/", -1)
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	log.Println("websocker connect: ", ws.RemoteAddr().String())
	c := &connection{send: make(chan []byte, 256), ws: ws}
	h.register <- c
	defer func() { h.unregister <- c }()
	go c.writer()
	c.reader()
}
func homeHandler(c http.ResponseWriter, req *http.Request) {
	t, err := template.ParseFiles(path.Join(templatePath, "/html/index2.html"))
	if err != nil {
		log.Println(err)
		return
	}
	t.Execute(c, nil)
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(time.Duration(10) * time.Second))

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, ttu104[U104].wr)
	l, err := conn.Write(buf.Bytes())
	if err != nil {
		log.Println("handleConnection ", conn.RemoteAddr().String(), " write ", err)
	} else {
		log.Println("handleConnection ", conn.RemoteAddr().String(), " write start cmd u ", l)
	}

	buffer := make([]byte, 2048)
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			log.Println("handleConnection ", conn.RemoteAddr().String(), "connection error: ", err)
			return
		}

		log.Printf("handleConnection %s receive data:%X \n", conn.RemoteAddr().String(), buffer[:n])

		/*
			for _, t := range ttu104 {
				if bytes.Equal(buffer[:n], t.rd) {
					buf := new(bytes.Buffer)
					binary.Write(buf, binary.LittleEndian, call104)
					l, err := conn.Write(buf.Bytes())
					if err != nil {
						log.Println("handleConnection ", conn.RemoteAddr().String(), " write call cmd ", err)
					} else {
						log.Println("handleConnection ", conn.RemoteAddr().String(), " write call cmd ", l)
					}
				}
			}*/
		if bytes.Equal(buffer[:n], ttu104[U104].rd) {
			log.Println("handleConnection recv start cmd u ok")

			buf := new(bytes.Buffer)
			binary.Write(buf, binary.LittleEndian, ttu104[CALL].wr)
			l, err := conn.Write(buf.Bytes())
			if err != nil {
				log.Println("handleConnection ", conn.RemoteAddr().String(), " write call cmd ", err)
			} else {
				log.Println("handleConnection ", conn.RemoteAddr().String(), " write call cmd ", l)
			}
		}
		if bytes.Equal(buffer[:n], ttu104[CALL].rd) {
			log.Printf("handleConnection %s receive call cmd ok :%X \n", conn.RemoteAddr().String(), buffer[:n])
		}
		if bytes.Equal(buffer[:n], ttu104[TEST].rd) {
			log.Printf("handleConnection %s receive heartbeat data:%X \n", conn.RemoteAddr().String(), buffer[:n])
		}

		h.broadcast <- buffer[:n]
		conn.SetReadDeadline(time.Now().Add(time.Duration(60) * time.Second))
	}
}

func handleHeartbeat(conn net.Conn) {
	timer := time.NewTicker(15 * time.Second)

LOOP:
	for {
		select {
		case <-timer.C:
			log.Println("handleHeartbeat ", conn.RemoteAddr().String(), " heartbeat")
			buf := new(bytes.Buffer)
			binary.Write(buf, binary.LittleEndian, ttu104[TEST].wr)
			l, err := conn.Write(buf.Bytes())
			if err != nil {
				log.Println("handleHeartbeat ", conn.RemoteAddr().String(), " write ", err)
				break LOOP
			} else {
				log.Println("handleHeartbeat", conn.RemoteAddr().String(), " write ", l)
			}
		}
	}
}

func main() {

	for _, t := range ttu104 {
		log.Println(t.rd, ":", bytesToString(t.rd), ",", t.wr, ":", bytesToString(t.wr))
	}

	netListen, err := net.Listen("tcp", "0.0.0.0:6025")
	if err != nil {
		log.Println("Fatal error: ", err.Error())
		return
	}
	defer netListen.Close()

	log.Println("start tcp server: listen port 6025")

	go func() {
		for {
			conn, err := netListen.Accept()
			if err != nil {
				continue
			}

			log.Println("tcp connect success: ", conn.RemoteAddr().String())
			go handleConnection(conn)
			go handleHeartbeat(conn)
		}
	}()

	go h.run()

	httpSrv := &http.Server{
		Addr:         ":8090",
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	templatePath = path.Join(getCurrentDirectory(), "template")
	http.Handle("/jquery-1.4.2/", http.FileServer(http.Dir(templatePath)))
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/ws", wsHandler)
	log.Println("start http server: listen port 8090")
	log.Println(templatePath)
	log.Fatal(httpSrv.ListenAndServe())
}
