package websocket

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"strings"
	"sync"
	"time"
)

type Hub struct {
	groups []*Group

	connect                  chan *Client
	disconnect               chan *Client
	broadcastToGroup         chan *frame
	broadcastToAllGroups     chan *frame
	broadcastToConnection    chan *frame
	broadcastToOthersInGroup chan *frame
}

type Group struct {
	Id      string
	Clients []*Client
}

type Client struct {
	GroupId      string
	ConnectionId string

	OnMessage      chan []byte `json:"-"`
	OnError        chan error  `json:"-"`
	LostConnection chan error  `json:"-"`

	connection WsReadWriteCloser
	lockR      sync.RWMutex
	lockW      sync.RWMutex
}

// WsReadWriteCloser is responsible to read and write messages from websocket connection.
type WsReadWriteCloser interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(data []byte) error
	Close() error
}

type frame struct {
	GroupId      string    `json:"group_id"`
	ConnectionId string    `json:"connection_id"`
	Content      []byte    `json:"content"`
	Time         time.Time `json:"time"`
}

func NewHub() *Hub {
	return &Hub{
		groups:                   make([]*Group, 0),
		connect:                  make(chan *Client),
		disconnect:               make(chan *Client),
		broadcastToGroup:         make(chan *frame),
		broadcastToAllGroups:     make(chan *frame),
		broadcastToConnection:    make(chan *frame),
		broadcastToOthersInGroup: make(chan *frame),
	}
}

func (hub *Hub) ListenForConnections(ctx context.Context) chan bool {
	finished := make(chan bool)

	go func(hub *Hub) {
		defer close(finished)

		for {
			select {
			case <-ctx.Done():
				return
			case client := <-hub.connect:
				hub.assignClientToGroup(client)
			case client := <-hub.disconnect:
				hub.disconnectClientFromGroup(client.GroupId, client.ConnectionId)
			case fr := <-hub.broadcastToGroup:
				hub.sendToGroup(fr.GroupId, fr.Content)
			case fr := <-hub.broadcastToAllGroups:
				hub.sendToAllGroups(fr.Content)
			case fr := <-hub.broadcastToConnection:
				hub.sendToConnection(fr.GroupId, fr.ConnectionId, fr.Content)
			case fr := <-hub.broadcastToOthersInGroup:
				hub.sendToOthersInGroup(fr.GroupId, fr.ConnectionId, fr.Content)
			}
		}
	}(hub)

	return finished
}

func (hub *Hub) EstablishConnection(conn WsReadWriteCloser, groupId, connectionId string) (*Client, error) {
	if strings.TrimSpace(connectionId) == "" {
		connectionId = uuid.New().String()
	}

	if hub.client(groupId, connectionId) != nil {
		return nil, errors.New(fmt.Sprintf("client %s already exists", connectionId))
	}

	client := &Client{
		OnMessage:      make(chan []byte),
		OnError:        make(chan error),
		LostConnection: make(chan error),
		GroupId:        groupId,
		ConnectionId:   connectionId,
		connection:     conn,
	}

	hub.connect <- client

	return client, nil
}

func (hub *Hub) DisconnectFromGroup(groupId, connectionId string) {
	hub.disconnect <- &Client{
		GroupId:      groupId,
		ConnectionId: connectionId,
	}
}

func (hub *Hub) SendToGroup(groupId string, msg []byte) {
	hub.broadcastToGroup <- &frame{
		GroupId: groupId,
		Content: msg,
		Time:    time.Now(),
	}
}

func (hub *Hub) SendToAllGroups(msg []byte) {
	hub.broadcastToAllGroups <- &frame{
		Content: msg,
		Time:    time.Now(),
	}
}

func (hub *Hub) SendToConnectionId(groupId, connectionId string, msg []byte) {
	hub.broadcastToConnection <- &frame{
		GroupId:      groupId,
		ConnectionId: connectionId,
		Content:      msg,
		Time:         time.Now(),
	}
}

func (hub *Hub) SendToOthersInGroup(groupId, connectionId string, msg []byte) {
	hub.broadcastToOthersInGroup <- &frame{
		GroupId:      groupId,
		ConnectionId: connectionId,
		Content:      msg,
		Time:         time.Now(),
	}
}

func (client *Client) ReadMessages(ctx context.Context) chan bool {
	finished := make(chan bool)

	go func(client *Client) {
		defer close(finished)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				_, msg, err := client.read()
				if err != nil {
					if lostConnection(err) && client.LostConnection != nil {
						client.LostConnection <- err
						continue
					}

					if client.OnError != nil {
						client.OnError <- err
					}
				}

				if client.OnMessage != nil {
					client.OnMessage <- msg
				}
			}
		}
	}(client)

	return finished
}

func (hub *Hub) Groups() []*Group {
	return hub.groups
}

func (hub *Hub) Group(groupId string) *Group {
	return hub.group(groupId)
}

func (hub *Hub) assignClientToGroup(client *Client) {
	var group *Group

	if group = hub.group(client.GroupId); group == nil {
		group = &Group{
			Id:      client.GroupId,
			Clients: make([]*Client, 0),
		}

		hub.groups = append(hub.groups, group)
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
			go func(client *Client, msg []byte) {
				if err := client.write(msg); err != nil {
					if errBrokenPipe(err) {
						logrus.Warnf("client [%v] will be disconnected from group [%v]", client.ConnectionId, client.GroupId)
						hub.disconnect <- client
					}

					return
				}
			}(client, msg)
		}
	}
}

func (hub *Hub) sendToAllGroups(msg []byte) {
	for _, group := range hub.groups {
		go func(group *Group, msg []byte) {
			for _, client := range group.Clients {
				go func(client *Client, msg []byte) {
					if err := client.write(msg); err != nil {
						if errBrokenPipe(err) {
							logrus.Warnf("client [%v] will be disconnected from group [%v]", client.ConnectionId, client.GroupId)
							hub.disconnect <- client
						}

						return
					}
				}(client, msg)
			}
		}(group, msg)
	}
}

func (hub *Hub) sendToConnection(groupId, connectionId string, msg []byte) {
	if client := hub.client(groupId, connectionId); client != nil {
		go func(client *Client, msg []byte) {
			if err := client.write(msg); err != nil {
				if errBrokenPipe(err) {
					logrus.Warnf("client [%v] will be disconnected from group [%v]", connectionId, groupId)
					hub.disconnect <- client
				}

				return
			}
		}(client, msg)
	}
}

func (hub *Hub) sendToOthersInGroup(groupId, connectionId string, msg []byte) {
	if group := hub.group(groupId); group != nil {
		for _, client := range group.Clients {
			if client.ConnectionId == connectionId {
				continue
			}

			go func(client *Client, msg []byte) {
				if err := client.write(msg); err != nil {
					if errBrokenPipe(err) {
						logrus.Warnf("client [%v] will be disconnected from group [%v]", connectionId, groupId)
						hub.disconnect <- client
					}

					return
				}
			}(client, msg)
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
	for _, group := range hub.groups {
		if group.Id == groupId {
			return group
		}
	}

	return nil
}

func (client *Client) read() (int, []byte, error) {
	client.lockR.Lock()
	defer client.lockR.Unlock()

	return client.connection.ReadMessage()
}

func (client *Client) write(data []byte) error {
	client.lockW.Lock()
	defer client.lockW.Unlock()

	return client.connection.WriteMessage(data)
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

func lostConnection(err error) bool {
	return errBrokenPipe(err) || errConnClosed(err) || errGoingAway(err) || errAbnormalClose(err)
}
