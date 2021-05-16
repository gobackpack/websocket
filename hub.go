package websocket

import (
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
	Connect    chan *Client
	Disconnect chan *Client
	Groups     []*Group

	BroadcastToGroup         chan *Frame
	BroadcastToAllGroups     chan *Frame
	BroadcastToConnection    chan *Frame
	BroadcastToOthersInGroup chan *Frame

	ClientClosedConn chan *Client
}

type Client struct {
	GroupId      string
	ConnectionId string

	Connection       *websocketLib.Conn `json:"-"`
	OnMessage        func([]byte) error `json:"-"`
	OnError          func(err error)    `json:"-"`
	StoppedListening chan bool          `json:"-"`
	ReadLock         sync.Mutex         `json:"-"`
	SendLock         sync.Mutex         `json:"-"`
}

type Group struct {
	Id      string
	Clients []*Client
}

type Frame struct {
	GroupId      string    `json:"group_id"`
	ConnectionId string    `json:"connection_id"`
	Content      []byte    `json:"content"`
	Time         time.Time `json:"time"`
}

func NewHub() *Hub {
	return &Hub{
		Connect:                  make(chan *Client),
		Disconnect:               make(chan *Client),
		ClientClosedConn:         make(chan *Client),
		Groups:                   make([]*Group, 0),
		BroadcastToGroup:         make(chan *Frame, 0),
		BroadcastToAllGroups:     make(chan *Frame, 0),
		BroadcastToConnection:    make(chan *Frame, 0),
		BroadcastToOthersInGroup: make(chan *Frame, 0),
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
			case client := <-hub.Disconnect: // user requested disconnect
				hub.disconnectClientFromGroup(client.GroupId, client.ConnectionId)
				break
			case client := <-hub.ClientClosedConn: // unexpected disconnect (ex: user closed tab - going away err)
				hub.disconnectClientFromGroup(client.GroupId, client.ConnectionId)
				break
			case frame := <-hub.BroadcastToGroup:
				hub.broadcastToGroup(frame.GroupId, frame.Content)
				break
			case frame := <-hub.BroadcastToAllGroups:
				hub.broadcastToAllGroups(frame.Content)
				break
			case frame := <-hub.BroadcastToConnection:
				hub.broadcastToConnection(frame.GroupId, frame.ConnectionId, frame.Content)
				break
			case frame := <-hub.BroadcastToOthersInGroup:
				hub.broadcastToOthersInGroup(frame.GroupId, frame.ConnectionId, frame.Content)
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
		GroupId:          groupId,
		ConnectionId:     uuid.New().String(),
		Connection:       conn,
		StoppedListening: make(chan bool),
	}

	if client.OnMessage == nil {
		client.OnMessage = client.onMessage
	}

	if client.OnError == nil {
		client.OnError = client.onError
	}

	hub.Connect <- client

	go client.readMessages(hub.ClientClosedConn)

	return client, nil
}

func (hub *Hub) DisconnectFromGroup(groupId, connectionId string) {
	client := &Client{
		GroupId:      groupId,
		ConnectionId: connectionId,
	}

	hub.Disconnect <- client
}

func (hub *Hub) SendToGroup(groupId string, msg []byte) {
	frame := &Frame{
		GroupId: groupId,
		Content: msg,
		Time:    time.Now(),
	}

	hub.BroadcastToGroup <- frame
}

func (hub *Hub) SendToAllGroups(msg []byte) {
	frame := &Frame{
		Content: msg,
		Time:    time.Now(),
	}

	hub.BroadcastToAllGroups <- frame
}

func (hub *Hub) SendToConnectionId(groupId, connectionId string, msg []byte) {
	frame := &Frame{
		GroupId:      groupId,
		ConnectionId: connectionId,
		Content:      msg,
		Time:         time.Now(),
	}

	hub.BroadcastToConnection <- frame
}

func (hub *Hub) SendToOthersInGroup(groupId, connectionId string, msg []byte) {
	frame := &Frame{
		GroupId:      groupId,
		ConnectionId: connectionId,
		Content:      msg,
		Time:         time.Now(),
	}

	hub.BroadcastToOthersInGroup <- frame
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
	if group := hub.group(groupId); group != nil {
		for i := 0; i < len(group.Clients); i++ {
			if group.Clients[i].ConnectionId == connectionId {
				// if there is error that connection is already closed, ignore it, continue with further function processing
				if err := group.Clients[i].Connection.Close(); err != nil && !errConnClosed(err) {
					// but if there is error and connection is not already closed, stop further function processing
					// it means connection close failed, something went wrong!
					logrus.Errorf("client [%v] from group [%v] failed to close websocket connection: [%v]", connectionId, groupId, err)
					return
				}

				<-group.Clients[i].StoppedListening

				logrus.Warnf("client [%v] closed websocket connection from group [%v]", connectionId, groupId)

				copy(group.Clients[i:], group.Clients[i+1:])
				group.Clients[len(group.Clients)-1] = nil
				group.Clients = group.Clients[:len(group.Clients)-1]

				logrus.Warnf("client [%v] disconnected from group [%v]", connectionId, groupId)
			}
		}
	}
}

func (hub *Hub) broadcastToGroup(groupId string, msg []byte) {
	if group := hub.group(groupId); group != nil {
		for _, client := range group.Clients {
			go func(client *Client) {
				if err := client.write(TextMessage, msg); err != nil {
					logrus.Error("BroadcastToGroup failed: ", err)

					if errBrokenPipe(err) {
						logrus.Warnf("client [%v] will be disconnected from group [%v]", client.ConnectionId, client.GroupId)
						hub.Disconnect <- client
					}

					return
				}
			}(client)
		}
	}
}

func (hub *Hub) broadcastToAllGroups(msg []byte) {
	for _, group := range hub.Groups {
		// NOTE: if necessary each group can be processed concurrently
		for _, client := range group.Clients {
			go func(client *Client) {
				if err := client.write(TextMessage, msg); err != nil {
					logrus.Error("BroadcastToAllGroups failed: ", err)

					if errBrokenPipe(err) {
						logrus.Warnf("client [%v] will be disconnected from group [%v]", client.ConnectionId, client.GroupId)
						hub.Disconnect <- client
					}

					return
				}
			}(client)
		}
	}
}

func (hub *Hub) broadcastToConnection(groupId, connectionId string, msg []byte) {
	if client := hub.client(groupId, connectionId); client != nil {
		go func() {
			if err := client.write(TextMessage, msg); err != nil {
				logrus.Error("BroadcastToConnection failed: ", err)

				if errBrokenPipe(err) {
					logrus.Warnf("client [%v] will be disconnected from group [%v]", connectionId, groupId)
					hub.Disconnect <- client
				}

				return
			}
		}()
	}
}

func (hub *Hub) broadcastToOthersInGroup(groupId, connectionId string, msg []byte) {
	if group := hub.group(groupId); group != nil {
		for _, client := range group.Clients {
			if client.ConnectionId == connectionId {
				continue
			}

			go func(client *Client) {
				if err := client.write(TextMessage, msg); err != nil {
					logrus.Error("broadcastToOthersInGroup failed: ", err)

					if errBrokenPipe(err) {
						logrus.Warnf("client [%v] will be disconnected from group [%v]", connectionId, groupId)
						hub.Disconnect <- client
					}

					return
				}
			}(client)
		}
	}
}

func (hub *Hub) client(groupId, connectionId string) *Client {
	if group := hub.group(groupId); group != nil {
		for _, client := range group.Clients {
			if client.ConnectionId == connectionId {
				return client
			}
		}
	}

	return nil
}

func (hub *Hub) group(groupId string) *Group {
	for _, group := range hub.Groups {
		if group.Id == groupId {
			return group
		}
	}

	return nil
}

func (client *Client) readMessages(unexpectedClose chan *Client) {
	defer func() {
		logrus.Warnf("client [%v] from group [%v] stopped reading websocket messages",
			client.GroupId, client.ConnectionId)

		client.StoppedListening <- true
	}()

	for {
		_, msg, err := client.read()
		if err != nil {
			client.OnError(err)

			if errGoingAway(err) {
				unexpectedClose <- &Client{
					GroupId:      client.GroupId,
					ConnectionId: client.ConnectionId,
				}
			}
			break
		}

		if err := client.OnMessage(msg); err != nil {
			client.OnError(err)
		}
	}
}

func (client *Client) read() (int, []byte, error) {
	client.ReadLock.Lock()
	t, p, err := client.Connection.ReadMessage()
	client.ReadLock.Unlock()

	return t, p, err
}

func (client *Client) write(messageType int, data []byte) error {
	client.SendLock.Lock()
	err := client.Connection.WriteMessage(messageType, data)
	client.SendLock.Unlock()

	return err
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

func errGoingAway(err error) bool {
	return strings.Contains(err.Error(), "close 1001 (going away)")
}
