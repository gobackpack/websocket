package websocket

import (
	"encoding/json"
	websocketLib "github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"net/http"
	"sync"
)

const (
	// TextMessage type
	TextMessage = websocketLib.TextMessage
	// BinaryMessage type
	BinaryMessage = websocketLib.BinaryMessage
)

var upgrader = websocketLib.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type Hub struct {
	Connect    chan Client
	Disconnect chan Client
	Clients    map[string]*websocketLib.Conn
	Message    chan *Frame
	ReadLock   sync.Mutex
	SendLock   sync.Mutex
}

type Client struct {
	ConnectionId string
	Connection   *websocketLib.Conn
}

type Frame struct {
	ConnectionId string
	Content      string
}

func NewHub() *Hub {
	return &Hub{
		Connect:    make(chan Client),
		Disconnect: make(chan Client),
		Clients:    make(map[string]*websocketLib.Conn, 0),
		Message:    make(chan *Frame, 0),
	}
}

func (hub *Hub) ListenConnections(done chan bool) chan bool {
	cancelled := make(chan bool)

	go func(done chan bool) {
		defer func() {
			cancelled <- true
		}()

		for {
			select {
			case gr := <-hub.Connect:
				hub.Clients[gr.ConnectionId] = gr.Connection
				break
			case gr := <-hub.Disconnect:
				delete(hub.Clients, gr.ConnectionId)
				break
			case m := <-hub.Message:
				b, err := json.Marshal(m)
				if err != nil {
					logrus.Error("failed to marshal hub message: ", err)
					break
				}

				conn := hub.Clients[m.ConnectionId]
				if conn == nil {
					logrus.Error("client connection is null")
					break
				}

				if err := hub.sendMessage(conn, TextMessage, b); err != nil {
					logrus.Error("failed to send message: ", err)
					break
				}
				break
			case <-done:
				return
			}
		}
	}(done)

	return cancelled
}

func (hub *Hub) EstablishConnection(w http.ResponseWriter, r *http.Request, connectionId string) error {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}

	hub.Connect <- Client{
		ConnectionId: connectionId,
		Connection:   conn,
	}

	go hub.ReadMessages(conn)

	return nil
}

func (hub *Hub) DisconnectFromHub(connectionId string) {
	hub.Disconnect <- Client{
		ConnectionId: connectionId,
	}
}

func (hub *Hub) ReadMessages(conn *websocketLib.Conn) {
	defer func() {
		logrus.Warn("websocket connection stopped reading messages")

		if err := conn.Close(); err != nil {
			logrus.Error("failed to close websocket connection: ", err)
			return
		}

		logrus.Warn("websocket connection closed")
	}()

	for {
		_, msg, err := hub.readMessage(conn)

		if err != nil {
			logrus.Error("error message from websocket: ", err)
			break
		}

		logrus.Info("message received: ", string(msg))
	}
}

func (hub *Hub) SendMessage(connectionId string, msg []byte) error {
	frame := &Frame{
		ConnectionId: connectionId,
		Content:      string(msg),
	}

	hub.Message <- frame

	return nil
}

func (hub *Hub) readMessage(conn *websocketLib.Conn) (int, []byte, error) {
	hub.ReadLock.Lock()
	t, p, err := conn.ReadMessage()
	hub.ReadLock.Unlock()

	return t, p, err
}

func (hub *Hub) sendMessage(conn *websocketLib.Conn, messageType int, data []byte) error {
	hub.SendLock.Lock()
	err := conn.WriteMessage(messageType, data)
	hub.SendLock.Unlock()

	return err
}
