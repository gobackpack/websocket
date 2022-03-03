package websocket

import (
	"context"
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
	Groups []*Group

	connect                  chan *Client
	disconnect               chan *Client
	clientGoingAway          chan *Client
	broadcastToGroup         chan *Frame
	broadcastToAllGroups     chan *Frame
	broadcastToConnection    chan *Frame
	broadcastToOthersInGroup chan *Frame

	lock sync.RWMutex
}

type Client struct {
	GroupId      string
	ConnectionId string

	OnMessage chan []byte `json:"-"`
	OnError   chan error  `json:"-"`

	connection       *websocketLib.Conn
	stoppedListening chan bool
	lock             sync.RWMutex
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
		Groups:                   make([]*Group, 0),
		connect:                  make(chan *Client),
		disconnect:               make(chan *Client),
		clientGoingAway:          make(chan *Client),
		broadcastToGroup:         make(chan *Frame, 0),
		broadcastToAllGroups:     make(chan *Frame, 0),
		broadcastToConnection:    make(chan *Frame, 0),
		broadcastToOthersInGroup: make(chan *Frame, 0),
	}
}

func (hub *Hub) ListenConnections(ctx context.Context) chan bool {
	cancelled := make(chan bool)

	go func(ctx context.Context) {
		defer func() {
			cancelled <- true
		}()

		for {
			select {
			case client := <-hub.connect:
				hub.assignClientToGroup(client)
				break
			case client := <-hub.disconnect: // user requested disconnect
				hub.disconnectClientFromGroup(client.GroupId, client.ConnectionId)
				break
			case client := <-hub.clientGoingAway: // user closed tab - going away
				hub.disconnectClientFromGroup(client.GroupId, client.ConnectionId)
				break
			case frame := <-hub.broadcastToGroup:
				hub.sendToGroup(frame.GroupId, frame.Content)
				break
			case frame := <-hub.broadcastToAllGroups:
				hub.sendToAllGroups(frame.Content)
				break
			case frame := <-hub.broadcastToConnection:
				hub.sendToConnection(frame.GroupId, frame.ConnectionId, frame.Content)
				break
			case frame := <-hub.broadcastToOthersInGroup:
				hub.sendToOthersInGroup(frame.GroupId, frame.ConnectionId, frame.Content)
				break
			case <-ctx.Done():
				return
			}
		}
	}(ctx)

	return cancelled
}

func (hub *Hub) EstablishConnection(w http.ResponseWriter, r *http.Request, groupId, connectionId string) (*Client, error) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(connectionId) == "" {
		connectionId = uuid.New().String()
	}

	client := &Client{
		GroupId:          groupId,
		ConnectionId:     connectionId,
		connection:       conn,
		stoppedListening: make(chan bool),
	}

	hub.connect <- client

	go client.readMessages(hub.clientGoingAway)

	return client, nil
}

func (hub *Hub) DisconnectFromGroup(groupId, connectionId string) {
	client := &Client{
		GroupId:      groupId,
		ConnectionId: connectionId,
	}

	hub.disconnect <- client
}

func (hub *Hub) SendToGroup(groupId string, msg []byte) {
	frame := &Frame{
		GroupId: groupId,
		Content: msg,
		Time:    time.Now(),
	}

	hub.broadcastToGroup <- frame
}

func (hub *Hub) SendToAllGroups(msg []byte) {
	frame := &Frame{
		Content: msg,
		Time:    time.Now(),
	}

	hub.broadcastToAllGroups <- frame
}

func (hub *Hub) SendToConnectionId(groupId, connectionId string, msg []byte) {
	frame := &Frame{
		GroupId:      groupId,
		ConnectionId: connectionId,
		Content:      msg,
		Time:         time.Now(),
	}

	hub.broadcastToConnection <- frame
}

func (hub *Hub) SendToOthersInGroup(groupId, connectionId string, msg []byte) {
	frame := &Frame{
		GroupId:      groupId,
		ConnectionId: connectionId,
		Content:      msg,
		Time:         time.Now(),
	}

	hub.broadcastToOthersInGroup <- frame
}

func (hub *Hub) assignClientToGroup(client *Client) {
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
				if err := group.Clients[i].connection.Close(); err != nil && !errConnClosed(err) {
					// but if there is error and connection is not already closed, stop further function processing
					// it means connection close failed, something went wrong!
					logrus.Errorf("client [%v] from group [%v] failed to close websocket connection: [%v]", connectionId, groupId, err)
					return
				}

				<-group.Clients[i].stoppedListening

				logrus.Warnf("client [%v] from group [%v] closed websocket connection", connectionId, groupId)

				copy(group.Clients[i:], group.Clients[i+1:])
				group.Clients[len(group.Clients)-1] = nil
				group.Clients = group.Clients[:len(group.Clients)-1]

				logrus.Warnf("client [%v] disconnected from group [%v]", connectionId, groupId)
			}
		}
	}
}

func (hub *Hub) sendToGroup(groupId string, msg []byte) {
	if group := hub.group(groupId); group != nil {
		for _, client := range group.Clients {
			go func(client *Client) {
				if err := client.write(TextMessage, msg); err != nil {
					//logrus.Error("broadcastToGroup failed: ", err)

					if errBrokenPipe(err) {
						logrus.Warnf("client [%v] will be disconnected from group [%v]", client.ConnectionId, client.GroupId)
						hub.disconnect <- client
					}

					return
				}
			}(client)
		}
	}
}

func (hub *Hub) sendToAllGroups(msg []byte) {
	for _, group := range hub.Groups {
		// NOTE: if necessary each group can be processed concurrently
		for _, client := range group.Clients {
			go func(client *Client) {
				if err := client.write(TextMessage, msg); err != nil {
					//logrus.Error("broadcastToAllGroups failed: ", err)

					if errBrokenPipe(err) {
						logrus.Warnf("client [%v] will be disconnected from group [%v]", client.ConnectionId, client.GroupId)
						hub.disconnect <- client
					}

					return
				}
			}(client)
		}
	}
}

func (hub *Hub) sendToConnection(groupId, connectionId string, msg []byte) {
	if client := hub.client(groupId, connectionId); client != nil {
		go func() {
			if err := client.write(TextMessage, msg); err != nil {
				//logrus.Error("broadcastToConnection failed: ", err)

				if errBrokenPipe(err) {
					logrus.Warnf("client [%v] will be disconnected from group [%v]", connectionId, groupId)
					hub.disconnect <- client
				}

				return
			}
		}()
	}
}

func (hub *Hub) sendToOthersInGroup(groupId, connectionId string, msg []byte) {
	if group := hub.group(groupId); group != nil {
		for _, client := range group.Clients {
			if client.ConnectionId == connectionId {
				continue
			}

			go func(client *Client) {
				if err := client.write(TextMessage, msg); err != nil {
					//logrus.Error("broadcastToOthersInGroup failed: ", err)

					if errBrokenPipe(err) {
						logrus.Warnf("client [%v] will be disconnected from group [%v]", connectionId, groupId)
						hub.disconnect <- client
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

func (client *Client) readMessages(clientGoingAway chan *Client) {
	defer func() {
		logrus.Warnf("client [%v] from group [%v] stopped reading websocket messages",
			client.ConnectionId, client.GroupId)

		client.stoppedListening <- true
	}()

	for {
		_, msg, err := client.read()
		if err != nil {
			if client.OnError != nil {
				client.OnError <- err
			}

			if errGoingAway(err) || errAbnormalClose(err) {
				clientGoingAway <- &Client{
					GroupId:      client.GroupId,
					ConnectionId: client.ConnectionId,
				}
			}

			break
		}

		if client.OnMessage != nil {
			client.OnMessage <- msg
		}
	}
}

func (client *Client) read() (int, []byte, error) {
	client.lock.Lock()
	defer client.lock.Unlock()

	return client.connection.ReadMessage()
}

func (client *Client) write(messageType int, data []byte) error {
	client.lock.Lock()
	defer client.lock.Unlock()

	return client.connection.WriteMessage(messageType, data)
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

func errAbnormalClose(err error) bool {
	return strings.Contains(err.Error(), "close 1006 (abnormal closure): unexpected EOF")
}
