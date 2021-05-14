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
	Groups                []*Group
	BroadcastToGroup      chan *Frame
	BroadcastToAllGroups  chan *Frame
	BroadcastToConnection chan *Frame
	ReadLock              sync.Mutex
	SendLock              sync.Mutex
}

type Client struct {
	GroupId      string
	ConnectionId string
	Connection   *websocketLib.Conn `json:"-"`
	OnMessage    func([]byte) error `json:"-"`
	OnError      func(err error)    `json:"-"`
}

type Group struct {
	Id      string
	Clients []*Client
}

type Sub struct {
	GroupId      string
	ConnectionId string
	Connection   *websocketLib.Conn
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
		Groups:                make([]*Group, 0),
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
				hub.assignConnectionToGroup(client)
				break
			case client := <-hub.Disconnect:
				hub.disconnectClientFromGroup(client.GroupId, client.ConnectionId)
				break
			case frame := <-hub.BroadcastToGroup:
				hub.broadcastToGroup(frame)
				break
			case frame := <-hub.BroadcastToAllGroups:
				hub.broadcastToAllGroups(frame)
				break
			case frame := <-hub.BroadcastToConnection:
				hub.broadcastToConnection(frame)
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

func (hub *Hub) group(groupId string) *Group {
	for _, group := range hub.Groups {
		if group.Id == groupId {
			return group
		}
	}

	return nil
}

func (hub *Hub) connection(groupId, connectionId string) *websocketLib.Conn {
	if group := hub.group(groupId); group != nil {
		for _, client := range group.Clients {
			if client.ConnectionId == connectionId {
				return client.Connection
			}
		}
	}

	return nil
}

func (hub *Hub) assignConnectionToGroup(client *Client) {
	var group *Group

	if group = hub.group(client.GroupId); group == nil {
		group = &Group{
			Id:      client.GroupId,
			Clients: make([]*Client, 0),
		}

		hub.Groups = append(hub.Groups, group)
	}

	group.Clients = append(group.Clients, client)

	logrus.Infof("client [%v] connected to group [%v]", client.ConnectionId, client.GroupId)
}

func (hub *Hub) disconnectClientFromGroup(groupId, connectionId string) {
	if conn := hub.connection(groupId, connectionId); conn != nil {
		// if there is error that connection is already closed, ignore it, continue with further function processing
		if err := conn.Close(); err != nil && !errConnClosed(err) {
			// but if there is error and connection is not already closed, stop further function processing
			// it means connection close failed, something went wrong!
			logrus.Errorf("client [%v] from group [%v] failed to close websocket connection: [%v]", connectionId, groupId, err)
			return
		}

		if group := hub.group(groupId); group != nil {
			for _, client := range group.Clients {
				if client.ConnectionId == connectionId {
					client = nil
					logrus.Warnf("client [%v] disconnected from group [%v]", connectionId, groupId)
				}
			}
		}
	}

	logrus.Info("after disconnect::")
	printGroups(hub)
}

func (hub *Hub) broadcastToGroup(frame *Frame) {
	if group := hub.group(frame.GroupId); group != nil {
		b, err := json.Marshal(frame)
		if err != nil {
			logrus.Error("failed to marshal hub message: ", err)
			return
		}

		for _, client := range group.Clients {
			go func(client *Client) {
				if err := hub.write(client.Connection, TextMessage, b); err != nil {
					logrus.Error("BroadcastToGroup failed: ", err)

					if errBrokenPipe(err) {
						logrus.Warnf("connection_id [%v] will be disconnected from group [%v]", client.ConnectionId, client.GroupId)
						hub.Disconnect <- client
					}

					return
				}
			}(client)
		}
	}
}

func (hub *Hub) broadcastToAllGroups(frame *Frame) {
	for _, group := range hub.Groups {
		frame.GroupId = group.Id

		b, err := json.Marshal(frame)
		if err != nil {
			logrus.Error("failed to marshal hub message: ", err)
			break
		}

		for _, client := range group.Clients {
			go func(client *Client) {
				if err := hub.write(client.Connection, TextMessage, b); err != nil {
					logrus.Error("BroadcastToAllGroups failed: ", err)

					if errBrokenPipe(err) {
						logrus.Warnf("connection_id [%v] will be disconnected from group [%v]", client.ConnectionId, client.GroupId)
						hub.Disconnect <- client
					}

					return
				}
			}(client)
		}
	}
}

func (hub *Hub) broadcastToConnection(frame *Frame) {
	if conn := hub.connection(frame.GroupId, frame.ConnectionId); conn != nil {
		b, err := json.Marshal(frame)
		if err != nil {
			logrus.Error("failed to marshal hub message: ", err)
			return
		}

		go func() {
			if err := hub.write(conn, TextMessage, b); err != nil {
				logrus.Error("BroadcastToConnection failed: ", err)

				if errBrokenPipe(err) {
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
}

func (client *Client) onMessage(msg []byte) error {
	logrus.Infof("client [%v] received message: %v", client.ConnectionId, string(msg))
	return nil
}

func (client *Client) onError(err error) {
	logrus.Errorf("client [%v] received error message: %v", client.ConnectionId, err)
}

func errBrokenPipe(err error) bool {
	return strings.Contains(err.Error(), "broken pipe")
}

func errConnClosed(err error) bool {
	return strings.Contains(err.Error(), "use of closed network connection")
}

func printGroups(hub *Hub) {
	for _, group := range hub.Groups {
		c, err := json.Marshal(group.Clients)
		if err != nil {
			logrus.Error("print failed: ", err)
		}
		logrus.Infof("groupId: [%v], group clients: %v", group.Id, string(c))
	}
}
