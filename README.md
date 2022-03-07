![alt Go](https://img.shields.io/github/go-mod/go-version/gobackpack/websocket)

Usage

```go
// initialize hub and start listening for connections
hub := websocket.NewHub()

hubCtx, hubCancel := context.WithCancel(context.Background())
hubFinished := hub.ListenConnections(hubCtx)

// create client and establish connection with ws hub
client, err := hub.EstablishConnection(c.Writer, c.Request, groupId, connId)
if err != nil {
    logrus.Errorf("failed to establish connection with groupId -> %s: %s", groupId, err)
    return
}

client.OnError = make(chan error)
client.OnMessage = make(chan []byte)
clientCtx, clientCancel := context.WithCancel(hubCtx)

clientFinished := client.ReadMessages(clientCtx)

// handle messages
go func (clientCancel context.CancelFunc, client *websocket.Client) {
    defer clientCancel()

    for {
        select {
        case msg := <-client.OnMessage:
            logrus.Infof("client %s received message: %s", client.ConnectionId, msg)
            // optionally pass message to other connections, groups...
            hub.SendToGroup(groupId, msg)
        case err := <-client.OnError:
            logrus.Errorf("client %s received error: %s", client.ConnectionId, err)
            return
        }
    }
}(clientCancel, client)

<-clientFinished
close(clientFinished)

// send message
hub.SendToGroup(groupId, []byte("message to group"))

hub.SendToAllGroups([]byte("message to all groups"))

hub.SendToConnectionId(groupId, connectionId, []byte("message to connection"))

hub.SendToOthersInGroup(groupId, client.ConnectionId, []byte("message to all connections from my group except myself"))

// disconnect
hub.DisconnectFromGroup(groupId, connectionId)

// close
hubCancel()
<-hubFinished
close(hubFinished)
```

![image](https://user-images.githubusercontent.com/8428635/119730949-a181f880-be76-11eb-9dcd-f4952342f3b8.png)

![image](https://user-images.githubusercontent.com/8428635/119730888-8adba180-be76-11eb-8f29-019cd7d42792.png)

#### Todo

* Make sure the following are thread-safe:

```go
client.read()
client.write()
hub.Groups
```
