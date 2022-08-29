![alt Go](https://img.shields.io/github/go-mod/go-version/gobackpack/websocket)

Work in progress...

Usage

```go
// initialize hub and start listening for connections
hub := websocket.NewHub()

hubCtx, hubCancel := context.WithCancel(context.Background())
hubFinished := hub.ListenForConnections(hubCtx)

// create client and establish connection with ws hub
client, err := hub.EstablishGorillaWsConnection(c.Writer, c.Request, groupId, connId)
if err != nil {
    logrus.Errorf("failed to establish connection with groupId -> %s: %s", groupId, err)
    return
}

clientCtx, clientCancel := context.WithCancel(hubCtx)
clientFinished := client.ReadMessages(clientCtx)

// handle messages
go func (clientCancel context.CancelFunc, client *websocket.Client) {
    defer clientCancel()

    for {
        select {
        case msg := <-client.OnMessage:
            logrus.Infof("client %s received message: %s", client.ConnectionId, msg)
            go hub.SendToGroup(groupId, msg)
        case err := <-client.OnError:
            logrus.Errorf("client %s received error: %s", client.ConnectionId, err)
        case err = <-client.LostConnection:
            hub.DisconnectFromGroup(client.GroupId, client.ConnectionId)
            return
        }
    }
}(clientCancel, client)

<-clientFinished

// send message
go hub.SendToGroup(groupId, []byte("message to group"))

go hub.SendToAllGroups([]byte("message to all groups"))

go hub.SendToConnectionId(groupId, connectionId, []byte("message to connection"))

go hub.SendToOthersInGroup(groupId, client.ConnectionId, []byte("message to all connections from my group except myself"))

// disconnect
hub.DisconnectFromGroup(groupId, connectionId)

// close
hubCancel()
<-hubFinished
```

#### Todo

* Make sure the following are thread-safe:

```go
client.read()
client.write()
hub.Groups
```
