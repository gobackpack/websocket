package websocket

import (
	"encoding/json"
	"github.com/google/uuid"
	websocketLib "github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"net/http"
	"strings"
	"sync"
	"time"
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
	Connect               chan *Client
	Disconnect            chan *Client
	Clients               map[string]map[string]*websocketLib.Conn
	BroadcastToGroup      chan *Frame
	BroadcastToAllGroups  chan *Frame
	BroadcastToConnection chan *Frame
	ReadLock              sync.Mutex
	SendLock              sync.Mutex
}

type Client struct {
	GroupId      string
	ConnectionId string
	Connection   *websocketLib.Conn
	OnMessage    func([]byte) error
	OnError      func(err error)
}

type Frame struct {
	GroupId      string    `json:"group_id"`
	ConnectionId string    `json:"connection_id"`
	Content      string    `json:"content"`
	Time         time.Time `json:"time"`
}

func NewHub() *Hub {
	return &Hub{
		Connect:               make(chan *Client),
		Disconnect:            make(chan *Client),
		Clients:               make(map[string]map[string]*websocketLib.Conn, 0),
		BroadcastToGroup:      make(chan *Frame, 0),
		BroadcastToAllGroups:  make(chan *Frame, 0),
		BroadcastToConnection: make(chan *Frame, 0),
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
				hub.assignConnectionToGroup(client.GroupId, client.ConnectionId, client.Connection)
				break
			case client := <-hub.Disconnect:
				hub.disconnectClientFromGroup(client.GroupId, client.ConnectionId)
				break
			case frame := <-hub.BroadcastToGroup:
				if group := hub.group(frame.GroupId); group != nil {
					b, err := json.Marshal(frame)
					if err != nil {
						logrus.Error("failed to marshal hub message: ", err)
						break
					}

					for connId, conn := range group {
						go func(connId string, conn *websocketLib.Conn) {
							if err := hub.write(conn, TextMessage, b); err != nil {
								logrus.Error("BroadcastToGroup failed: ", err)

								if strings.Contains(err.Error(), "broken pipe") {
									logrus.Warnf("connection_id [%v] will be disconnected from group [%v]", connId, frame.GroupId)
									hub.Disconnect <- &Client{
										GroupId:      frame.GroupId,
										ConnectionId: connId,
									}
								}

								return
							}
						}(connId, conn)
					}
				}
				break
			case frame := <-hub.BroadcastToAllGroups:
				for groupId, connections := range hub.Clients {
					frame.GroupId = groupId

					b, err := json.Marshal(frame)
					if err != nil {
						logrus.Error("failed to marshal hub message: ", err)
						break
					}

					for connId, conn := range connections {
						go func(connId string, conn *websocketLib.Conn) {
							if err := hub.write(conn, TextMessage, b); err != nil {
								logrus.Error("BroadcastToAllGroups failed: ", err)

								if strings.Contains(err.Error(), "broken pipe") {
									logrus.Warnf("connection_id [%v] will be disconnected from group [%v]", connId, frame.GroupId)
									hub.Disconnect <- &Client{
										GroupId:      frame.GroupId,
										ConnectionId: connId,
									}
								}

								return
							}
						}(connId, conn)
					}
				}
				break
			case frame := <-hub.BroadcastToConnection:
				if conn := hub.connection(frame.GroupId, frame.ConnectionId); conn != nil {
					b, err := json.Marshal(frame)
					if err != nil {
						logrus.Error("failed to marshal hub message: ", err)
						break
					}

					go func() {
						if err := hub.write(conn, TextMessage, b); err != nil {
							logrus.Error("BroadcastToConnection failed: ", err)

							if strings.Contains(err.Error(), "broken pipe") {
								logrus.Warnf("connection_id [%v] will be disconnected from group [%v]", frame.ConnectionId, frame.GroupId)
								hub.Disconnect <- &Client{
									GroupId:      frame.GroupId,
									ConnectionId: frame.ConnectionId,
								}
							}

							return
						}
					}()
				}
				break
			case <-done:
				return
			}
		}
	}(done)

	return cancelled
}

func (hub *Hub) EstablishConnection(w http.ResponseWriter, r *http.Request, groupId string) (*Client, error) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return nil, err
	}

	client := &Client{
		GroupId:      groupId,
		ConnectionId: uuid.New().String(),
		Connection:   conn,
	}

	if client.OnMessage == nil {
		client.OnMessage = client.onMessage
	}

	if client.OnError == nil {
		client.OnError = client.onError
	}

	hub.Connect <- client

	go hub.ReadMessages(client)

	return client, nil
}

func (hub *Hub) DisconnectFromGroup(groupId string, connectionId string) {
	client := &Client{
		GroupId:      groupId,
		ConnectionId: connectionId,
	}

	hub.Disconnect <- client
}

func (hub *Hub) ReadMessages(client *Client) {
	defer func() {
		logrus.Warnf("websocket connection stopped reading messages: groupId[%v] -> connectionId[%v]",
			client.GroupId, client.ConnectionId)

		hub.Disconnect <- client
	}()

	for {
		_, msg, err := hub.read(client.Connection)

		if err != nil {
			client.OnError(err)
			break
		}

		if err := client.OnMessage(msg); err != nil {
			client.OnError(err)
		}
	}
}

func (hub *Hub) SendToGroup(groupId string, msg []byte) {
	frame := &Frame{
		GroupId: groupId,
		Content: string(msg),
		Time:    time.Now(),
	}

	hub.BroadcastToGroup <- frame
}

func (hub *Hub) SendToAllGroups(msg []byte) {
	frame := &Frame{
		Content: string(msg),
		Time:    time.Now(),
	}

	hub.BroadcastToAllGroups <- frame
}

func (hub *Hub) SendToConnectionId(groupId string, connectionId string, msg []byte) {
	frame := &Frame{
		GroupId:      groupId,
		ConnectionId: connectionId,
		Content:      string(msg),
		Time:         time.Now(),
	}

	hub.BroadcastToConnection <- frame
}

func (hub *Hub) read(conn *websocketLib.Conn) (int, []byte, error) {
	hub.ReadLock.Lock()
	t, p, err := conn.ReadMessage()
	hub.ReadLock.Unlock()

	return t, p, err
}

func (hub *Hub) write(conn *websocketLib.Conn, messageType int, data []byte) error {
	hub.SendLock.Lock()
	err := conn.WriteMessage(messageType, data)
	hub.SendLock.Unlock()

	return err
}

func (client *Client) onMessage(msg []byte) error {
	logrus.Infof("client [%v] received message: %v", client.ConnectionId, string(msg))
	return nil
}

func (client *Client) onError(err error) {
	logrus.Errorf("client [%v] received error message: %v", client.ConnectionId, err)
}

func (hub *Hub) group(groupId string) map[string]*websocketLib.Conn {
	return hub.Clients[groupId]
}

func (hub *Hub) connection(groupId, connectionId string) *websocketLib.Conn {
	if hub.group(groupId) != nil {
		return hub.Clients[groupId][connectionId]
	}

	return nil
}

func (hub *Hub) assignConnectionToGroup(groupId string, connectionId string, conn *websocketLib.Conn) {
	hub.createGroupIfNotExists(groupId)
	hub.Clients[groupId][connectionId] = conn
	logrus.Infof("client [%v] connected to group [%v]", connectionId, groupId)
}

func (hub *Hub) disconnectClientFromGroup(groupId, connectionId string) {
	if conn := hub.connection(groupId, connectionId); conn != nil {
		if err := conn.Close(); err != nil {
			logrus.Errorf("client [%v] failed to disconnect from group [%v]", connectionId, groupId)
			return
		}

		delete(hub.group(groupId), connectionId)
		logrus.Warnf("client [%v] disconnected from group [%v]", connectionId, groupId)
	}
}

func (hub *Hub) createGroupIfNotExists(groupId string) {
	if hub.group(groupId) == nil {
		hub.Clients[groupId] = make(map[string]*websocketLib.Conn, 0)
	}
}
