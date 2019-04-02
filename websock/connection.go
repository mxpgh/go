package main

import (
	"fmt"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

type connection struct {
	// websocket 连接器
	ws *websocket.Conn

	// 发送信息的缓冲 channel
	send chan []byte
}

func (c *connection) reader() {
	for {
		_, message, err := c.ws.ReadMessage()
		if err != nil {
			log.Println("websocket read error: ", err)
			break
		}
		if string(message) == "ping" {
			c.ws.WriteMessage(websocket.TextMessage, []byte("pong"))
			log.Println("websocket recv ping: ", c.ws.RemoteAddr().String())
		}
		//h.broadcast <- message
	}
	c.ws.Close()
	log.Println("websocket read close: ", c.ws.RemoteAddr().String())
}

func (c *connection) writer() {
LOOP:
	for {
		select {
		case data, ok := <-c.send:
			{
				if !ok {
					log.Println("websocket write chan close: ", c.ws.RemoteAddr().String())
					break LOOP
				}
				strData := bytesToString(data) //hex.EncodeToString(data)
				log.Println("wtiter ", data, ": ", strData)
				err := c.ws.WriteMessage(websocket.TextMessage, []byte(strData))
				if err != nil {
					log.Println("websocket write error: ", err)
					break
				}
			}

		case <-time.After(time.Second * 90):
			//log.Println("websocket write chan timeout ")
			//break LOOP
		}
	}
	/*
		for message := range c.send {
			err := c.ws.WriteMessage(websocket.TextMessage, message)
			if err != nil {
				break
			}
		}*/
	c.ws.Close()
	log.Println("websocker write close: ", c.ws.RemoteAddr().String())
}

func bytesToString(data []byte) string {
	var ret string
	for _, b := range data {
		ret += fmt.Sprintf("%02X ", b)
	}
	return ret
}
