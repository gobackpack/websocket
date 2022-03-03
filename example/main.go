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
	router.LoadHTMLFiles("index.html")

	pprof.Register(router)

	// serve frontend
	// this url will call /ws/:groupId (from frontend) which is going to establish ws connection
	router.GET("/join/:groupId", func(c *gin.Context) {
		c.HTML(200, "index.html", nil)
	})

	hub := websocket.NewHub()

	hubCtx, hubCancel := context.WithCancel(context.Background())
	hubCancelled := hub.ListenConnections(hubCtx)

	// connect client to group
	router.GET("/ws/:groupId", func(c *gin.Context) {
		groupId := c.Param("groupId")

		// NOTE: if connectionId is "", uuid will be automatically generated
		// find your own way to return client.ConnectionId to frontend
		// client.ConnectionId is required for manual /disconnect

		client, err := hub.EstablishConnection(c.Writer, c.Request, groupId, "")
		if err != nil {
			logrus.Errorf("failed to establish connection with groupId -> %s: %s", groupId, err)
			return
		}

		client.OnError = make(chan error)
		client.OnMessage = make(chan []byte)
		clientCtx, clientCancel := context.WithCancel(hubCtx)

		clientCancelled := client.ReadMessages(clientCtx)

		go func(clientCancel context.CancelFunc) {
			for {
				select {
				case msg := <-client.OnMessage:
					logrus.Infof("client %s received message: %s", client.ConnectionId, msg)
					break
				case err := <-client.OnError:
					logrus.Errorf("client %s received error: %s", client.ConnectionId, err)
					break
					//clientCancel() // we choose when to stop reading messages from ws
					//return
				case <-time.After(5 * time.Second):
					clientCancel() // only simulation to stop reading messages from ws
					return
				}
			}
		}(clientCancel)

		logrus.Infof("client %s listening for messages...", client.ConnectionId)

		<-clientCancelled

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
		c.JSON(http.StatusOK, hub.Groups)
	})

	//router.POST("/sendMessage", func(c *gin.Context) {
	//	groupId := c.GetHeader("group_id")
	//	if strings.TrimSpace(groupId) == "" {
	//		c.JSON(http.StatusBadRequest, "missing group_id from headers")
	//		return
	//	}
	//
	//	wg := sync.WaitGroup{}
	//
	//	wg.Add(100)
	//	for i := 0; i < 100; i++ {
	//		go func(wg *sync.WaitGroup) {
	//			hub.SendToGroup(groupId, []byte(fmt.Sprintf("groupId [%v]", groupId)))
	//			wg.Done()
	//		}(&wg)
	//	}
	//
	//	wg.Wait()
	//
	//	logrus.Info("all messages sent")
	//})

	httpServe(router, "", "8080")
	hubCancel()

	<-hubCancelled

	logrus.Warn("application stopped")
}

func httpServe(router *gin.Engine, host, port string) {
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
