package websocket
//
//import (
//	"github.com/sirupsen/logrus"
//
//	websocketLib "github.com/gorilla/websocket"
//)
//
//// Client for websocket
//type Client struct {
//	MessageHandler
//	Channel        *Channel
//	DisabledReader bool
//}
//
//// Connect will create websocket Client, connect to ws url
//// url: ws://localhost:8080
//func (client *Client) Connect(done chan bool, ready chan bool, url string) {
//	if client.MessageHandler == nil {
//		logrus.Fatal("MessageHandler is missing")
//	}
//
//	conn := client.connection(url)
//
//	ch := &Channel{
//		Connection:     conn,
//		MessageHandler: client.MessageHandler,
//	}
//
//	client.Channel = ch
//
//	if !client.DisabledReader {
//		go ch.read()
//	}
//
//	ready <- true
//
//	logrus.Info("connected to: ", url)
//
//	<-done
//
//	logrus.Warn("client returned")
//}
//
//// SendText message to websocket channel
//func (client *Client) SendText(msg []byte) error {
//	return client.Channel.sendMessage(TextMessage, msg)
//}
//
//// SendBinary message to websocket channel
//func (client *Client) SendBinary(msg []byte) error {
//	return client.Channel.sendMessage(BinaryMessage, msg)
//}
//
//// connection is helper function to create gorilla websocket connection
//func (client *Client) connection(url string) *websocketLib.Conn {
//	conn, _, err := websocketLib.DefaultDialer.Dial(url, nil)
//	if err != nil {
//		logrus.Fatal("websocket dialer failed: ", err)
//	}
//
//	return conn
//}
