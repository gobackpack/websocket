package tick

import (
	"fmt"
	"github.com/gobackpack/websocket"
	"time"
)

type Ticker struct {
	Hub      *websocket.Hub
	StopTick chan bool
}

func NewTicker(hub *websocket.Hub) *Ticker {
	return &Ticker{
		Hub:      hub,
		StopTick: make(chan bool),
	}
}

func (ticker *Ticker) Start() {
	go func() {
		count := 0
		for {
			select {
			case <-time.After(time.Second):
				ticker.Hub.SendToAllGroups([]byte(fmt.Sprint(count)))
				count++
				break
			case <-ticker.StopTick:
				return
			}
		}
	}()
}

func (ticker *Ticker) Stop() {
	ticker.StopTick <- true
}
