package websocket

import (
	websocketLib "github.com/gorilla/websocket"
	"net/http"
)

type GorillaAdapter struct {
	conn *websocketLib.Conn
}

func NewGorillaConnectionAdapter(writer http.ResponseWriter, request *http.Request) (*GorillaAdapter, error) {
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
