Usage

```go
// initialize hub and start listening for connections
hub := websocket.NewHub()

done := make(chan bool)
cancelled := hub.ListenConnections(done)

// establish connection
client, err := hub.EstablishConnection(ctx.Writer, ctx.Request, groupId, "")

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