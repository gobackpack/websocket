package websocket

import (
	"encoding/json"
	"github.com/google/uuid"
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
	Connect    chan *Client
	Disconnect chan *Client
	Clients    map[string]map[string]*websocketLib.Conn
	Message    chan *Frame
	ReadLock   sync.Mutex
	SendLock   sync.Mutex
}

type Client struct {
	GroupId      string
	ConnectionId string
	Connection   *websocketLib.Conn
}

type Frame struct {
	GroupId string
	Content string
}

func NewHub() *Hub {
	return &Hub{
		Connect:    make(chan *Client),
		Disconnect: make(chan *Client),
		Clients:    make(map[string]map[string]*websocketLib.Conn, 0),
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
			case client := <-hub.Connect:
				if hub.Clients[client.GroupId] == nil {
					hub.Clients[client.GroupId] = make(map[string]*websocketLib.Conn, 0)
				}
				clientId := uuid.New().String()
				hub.Clients[client.GroupId][clientId] = client.Connection
				logrus.Infof("client [%v] connected to group [%v]", clientId, client.GroupId)
				break
			case client := <-hub.Disconnect:
				if hub.Clients[client.GroupId] != nil {
					delete(hub.Clients[client.GroupId], client.ConnectionId)
					logrus.Warnf("client [%v] disconnected from group [%v]: ", client.ConnectionId, client.GroupId)
				}
				break
			case frame := <-hub.Message:
				if hub.Clients[frame.GroupId] != nil {
					b, err := json.Marshal(frame)
					if err != nil {
						logrus.Error("failed to marshal hub message: ", err)
						break
					}

					group := hub.Clients[frame.GroupId]
					if group == nil {
						logrus.Error("client group is null")
						break
					}

					for _, conn := range group {
						if err := hub.send(conn, TextMessage, b); err != nil {
							logrus.Error("failed to send message: ", err)
							break
						}
					}
				}
				break
			case <-done:
				return
			}
		}
	}(done)

	return cancelled
}

func (hub *Hub) EstablishConnection(w http.ResponseWriter, r *http.Request, groupId string) error {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}

	client := &Client{
		GroupId:    groupId,
		Connection: conn,
	}

	hub.Connect <- client

	go hub.ReadMessages(client)

	return nil
}

func (hub *Hub) ReadMessages(client *Client) {
	defer func() {
		hub.Disconnect <- client

		logrus.Warn("websocket connection stopped reading messages")

		if err := client.Connection.Close(); err != nil {
			logrus.Error("failed to close websocket connection: ", err)
		}

		logrus.Warn("websocket connection closed")
	}()

	for {
		_, msg, err := hub.read(client.Connection)

		if err != nil {
			logrus.Error("error message from websocket: ", err)
			break
		}

		logrus.Info("message received: ", string(msg))
	}
}

func (hub *Hub) SendMessage(groupId string, msg []byte) error {
	frame := &Frame{
		GroupId: groupId,
		Content: string(msg),
	}

	hub.Message <- frame

	return nil
}

func (hub *Hub) read(conn *websocketLib.Conn) (int, []byte, error) {
	hub.ReadLock.Lock()
	t, p, err := conn.ReadMessage()
	hub.ReadLock.Unlock()

	return t, p, err
}

func (hub *Hub) send(conn *websocketLib.Conn, messageType int, data []byte) error {
	hub.SendLock.Lock()
	err := conn.WriteMessage(messageType, data)
	hub.SendLock.Unlock()

	return err
}
