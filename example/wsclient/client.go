package wsclient

import (
	"fmt"
	websocketLib "github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"sync"
)

type Client struct {
	Conn *websocketLib.Conn
	L    sync.Mutex
}

func NewClient() *Client {
	return &Client{}
}

func (client *Client) Spam() {
	conn, _, err := websocketLib.DefaultDialer.Dial("ws://localhost:8080/ws/1", nil)
	if err != nil {
		logrus.Fatal("connection to ws failed: ", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			logrus.Error("failed to close connection from ws: ", err)
		}

		logrus.Warn("closed connection to ws")
	}()

	client.Conn = conn

	delta := 500
	for j := 0; j < delta; j++ {
		if err = client.write(websocketLib.TextMessage, []byte(fmt.Sprint("msg ", j))); err != nil {
			logrus.Error(err)
		}
	}

	logrus.Infof("sent [%v] messages", delta)
}

func (client *Client) write(messageType int, data []byte) error {
	client.L.Lock()
	err := client.Conn.WriteMessage(messageType, data)
	client.L.Unlock()

	return err
}
