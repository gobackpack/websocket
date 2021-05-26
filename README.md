Usage

```go
// initialize hub and start listening for connections
hub := websocket.NewHub()

done := make(chan bool)
cancelled := hub.ListenConnections(done)

// establish connection
client, err := hub.EstablishConnection(ctx.Writer, ctx.Request, groupId, "")
// NOTE: required for now, listen for messages
d := make(chan bool)
counter := 0
go func () {
    defer func () {
        close(d)
        logrus.Warn("closed d")
    }()
    
    for {
        select {
        case msg, ok := <-c.OnMessage:
            if !ok {
            return
            }
            hub.SendToAllGroups(msg)
            counter++
            break
        case <-c.OnError:
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