package main

import (
	"errors"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/gobackpack/websocket"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	router := gin.New()

	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.LoadHTMLFiles("example/index.html")

	pprof.Register(router)

	// serve frontend
	// this url will call /ws/:groupId (from frontend) which is going to establish ws connection
	router.GET("/join/:groupId", func(c *gin.Context) {
		c.HTML(200, "index.html", nil)
	})

	hub := websocket.NewHub()

	hubCtx, hubCancel := context.WithCancel(context.Background())
	hubFinished := hub.ListenForConnections(hubCtx)

	// connect client to group
	router.GET("/ws/:groupId", func(c *gin.Context) {
		groupId := c.Param("groupId")
		connId, ok := c.GetQuery("connId")
		if !ok {
			connId = ""
		}

		// NOTE: if connectionId is "", uuid will be automatically generated
		// find your own way to return client.ConnectionId to frontend,
		// normally userId from jwt will be used.
		// client.ConnectionId is required for manual /disconnect

		client, err := hub.EstablishGorillaWsConnection(c.Writer, c.Request, groupId, connId)
		if err != nil {
			logrus.Errorf("failed to establish connection with groupId -> %s: %s", groupId, err)
			return
		}

		// send generated connection id back to frontend
		// this means userId from jwt was not used, let frontend know about generated connId
		if connId == "" {
			hub.SendToConnectionId(groupId, client.ConnectionId, []byte(client.ConnectionId))
		}

		clientCtx, clientCancel := context.WithCancel(hubCtx)
		clientFinished := client.ReadMessages(clientCtx)

		// handle messages from frontend
		go func(clientCancel context.CancelFunc, client *websocket.Client) {
			defer clientCancel()

			for {
				select {
				case msg := <-client.OnMessage:
					go hub.SendToGroup(groupId, msg)
				case err = <-client.OnError:
					logrus.Errorf("client %s received error: %s", client.ConnectionId, err)
				case err = <-client.LostConnection:
					hub.DisconnectFromGroup(client.GroupId, client.ConnectionId)
					return
				}
			}
		}(clientCancel, client)

		logrus.Infof("client %s listening for messages...", client.ConnectionId)

		<-clientFinished

		logrus.Warnf("client %s stopped reading messages from ws", client.ConnectionId)
	})

	// disconnect client from group
	router.POST("/disconnect", func(c *gin.Context) {
		groupId := c.GetHeader("group_id")
		if strings.TrimSpace(groupId) == "" {
			c.JSON(http.StatusBadRequest, "missing group_id from headers")
			return
		}

		connId := c.GetHeader("connection_id")
		if strings.TrimSpace(connId) == "" {
			c.JSON(http.StatusBadRequest, "missing connection_id from headers")
			return
		}

		hub.DisconnectFromGroup(groupId, connId)
	})

	// get all groups and clients
	router.GET("/connections", func(c *gin.Context) {
		c.JSON(http.StatusOK, hub.Groups())
	})

	// get all groups and clients
	router.GET("/connections/:groupId", func(c *gin.Context) {
		groupId := c.Param("groupId")

		c.JSON(http.StatusOK, hub.Group(groupId))
	})

	httpServe(router, "", "8080")
	hubCancel()

	<-hubFinished

	logrus.Warn("application stopped")
}

func httpServe(router http.Handler, host, port string) {
	addr := host + ":" + port

	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		logrus.Info("http listen: ", addr)

		if err := srv.ListenAndServe(); err != nil && errors.Is(err, http.ErrServerClosed) {
			logrus.Error("server listen err: ", err)
		}
	}()

	quit := make(chan os.Signal)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logrus.Warn("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logrus.Fatal("server forced to shutdown: ", err)
	}

	logrus.Warn("http server exited")
}
