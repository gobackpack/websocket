## Usage

### Client

* **Create ws.Client**
```
client := &ws.Client{
	MessageHandler: &mHandler{},
}

done := make(chan bool)
ready := make(chan bool)

go client.Connect(done, ready, "ws://localhost:8080")

<-ready

// ready to send messages to websocket channel
for i := 0; i < 10000; i++ {
    if err := client.SendText([]byte(fmt.Sprint("message: ", i))); err != nil {
        logrus.Fatal("failed to send message: ", err)
    }
}

close(done)

<-done
```

### Server

* **Create ws.Server**
```
server := &ws.Server{
	Host:     "localhost",
	Port:     "8080",
	Endpoint: "/",
	MessageHandler: &mHandler{},
}

done := make(chan bool)

go server.Run(done)

<-done
```

### Handler example
```
// mHandler example impl
type mHandler struct{
	counter int
}

func (h *mHandler) OnMessage(in []byte, reply func(int, []byte) error) {
	h.counter++
}

func (h *mHandler) OnError(err error) {
	// handle error from ws channel
	logrus.Error("error from ws connection: ", err)
	logrus.Info("received total: ", h.counter)
}
```
