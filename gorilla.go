package websocket

import (
	websocketLib "github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"net/http"
)

type GorillaAdapter struct {
	conn *websocketLib.Conn
}

func (hub *Hub) EstablishGorillaWsConnection(writer http.ResponseWriter, request *http.Request, groupId, connectionId string) (*Client, error) {
	conn, err := newGorillaConnectionAdapter(writer, request)
	if err != nil {
		logrus.Errorf("failed to upgrade connection: %s", err)
		return nil, err
	}

	return hub.EstablishConnection(conn, groupId, connectionId)
}

func newGorillaConnectionAdapter(writer http.ResponseWriter, request *http.Request) (*GorillaAdapter, error) {
	upgrader := websocketLib.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	conn, err := upgrader.Upgrade(writer, request, nil)

	return &GorillaAdapter{
		conn: conn,
	}, err
}

func (adapter *GorillaAdapter) ReadMessage() (int, []byte, error) {
	return adapter.conn.ReadMessage()
}

func (adapter *GorillaAdapter) WriteMessage(data []byte) error {
	return adapter.conn.WriteMessage(websocketLib.TextMessage, data)
}

func (adapter *GorillaAdapter) Close() error {
	return adapter.conn.Close()
}
