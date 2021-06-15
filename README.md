![alt Go](https://img.shields.io/github/go-mod/go-version/gobackpack/websocket)

Usage

```go
// initialize hub and start listening for connections
hub := websocket.NewHub()

done := make(chan bool)
cancelled := hub.ListenConnections(done)

// establish connection
client, err := hub.EstablishConnection(ctx.Writer, ctx.Request, groupId, "")
client.OnMessage = make(chan []byte)
client.OnError = make(chan error)

// NOTE: if OnMessage and OnError provided, then this listener is required, for now
d := make(chan bool)
counter := 0
go func () {
    defer func () {
        close(d)
        logrus.Warn("closed d")
    }()
    
    for {
        select {
        case msg, ok := <-client.OnMessage:
            if !ok {
                return
            }
            hub.SendToAllGroups(msg)
            counter++
            break
        case <-client.OnError:
            return
        }
    }
}()

<-d

logrus.Infof("received %v message", counter)

// send message
hub.SendToGroup(groupId, []byte("message to group"))

hub.SendToAllGroups([]byte("message to all groups"))

hub.SendToConnectionId(groupId, connectionId, []byte("message to connection"))

hub.SendToOthersInGroup(groupId, client.ConnectionId, []byte("message to all connections from my group except myself"))

// disconnect
hub.DisconnectFromGroup(groupId, connectionId)

// close
close(done)
<-cancelled
```


![image](https://user-images.githubusercontent.com/8428635/119730949-a181f880-be76-11eb-9dcd-f4952342f3b8.png)

![image](https://user-images.githubusercontent.com/8428635/119730888-8adba180-be76-11eb-8f29-019cd7d42792.png)
