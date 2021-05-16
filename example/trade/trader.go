package trade

import (
	"encoding/json"
	"github.com/gobackpack/websocket"
	websocketLib "github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

type Trader struct {
	Hub           *websocket.Hub
	StopListening chan bool
}

func NewTrader(hub *websocket.Hub) *Trader {
	return &Trader{
		Hub:           hub,
		StopListening: make(chan bool),
	}
}

type Subscription struct {
	Type string
	Data []*Trade
}

type Trade struct {
	S string
	P float32
	T uint
	V float32
}

func (trader *Trader) Start() {
	go func() {
		conn, _, err := websocketLib.DefaultDialer.Dial("wss://ws.finnhub.io?token=c2gi49iad3ie55jbbfug", nil)
		if err != nil {
			logrus.Fatal("connection to finnhub failed: ", err)
		}
		defer func() {
			if err := conn.Close(); err != nil {
				logrus.Error("failed to close connection from finnhub: ", err)
			}

			logrus.Warn("closed connection to finnhub")
		}()

		symbols := []string{"BINANCE:BTCUSDT", "IC MARKETS:1"}
		for _, s := range symbols {
			msg, _ := json.Marshal(map[string]interface{}{"type": "subscribe", "symbol": s})
			if err := conn.WriteMessage(websocketLib.TextMessage, msg); err != nil {
				logrus.Error("subscribe to finnhub failed: ", err)
			}
		}

		for {
			select {
			case <-trader.StopListening:
				return
			default:
				_, msg, err := conn.ReadMessage()
				if err != nil {
					logrus.Fatal("subscription failed: ", err)
				}

				var sub *Subscription
				if err := json.Unmarshal(msg, &sub); err != nil {
					logrus.Error(err)
					continue
				}

				for _, s := range sub.Data {
					payload := &struct {
						Symbol string
						Price  float32
					}{
						Symbol: s.S,
						Price:  s.P,
					}

					b, err := json.Marshal(payload)
					if err != nil {
						logrus.Error(err)
						continue
					}

					trader.Hub.SendToAllGroups(b)
				}
			}
		}
	}()
}

func (trader *Trader) Stop() {
	trader.StopListening <- true
}
