package main

import (
	"errors"
	"fmt"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/gobackpack/websocket"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
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

	ctxMain, cancel := context.WithCancel(context.Background())
	cancelled := hub.ListenConnections(ctxMain)

	// connect client to group
	router.GET("/ws/:groupId", func(c *gin.Context) {
		groupId := c.Param("groupId")

		// NOTE: if connectionId is "", uuid will be automatically generated
		// find your own way to return client.ConnectionId to frontend
		// client.ConnectionId is required for manual /disconnect

		_, err := hub.EstablishConnection(c.Writer, c.Request, groupId, "")
		if err != nil {
			logrus.Errorf("failed to establish connection with groupId -> %s: %s", groupId, err)
			return
		}
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

	// examples
	router.POST("/sendMessage", func(c *gin.Context) {
		groupId := c.GetHeader("group_id")
		if strings.TrimSpace(groupId) == "" {
			c.JSON(http.StatusBadRequest, "missing group_id from headers")
			return
		}

		wg := sync.WaitGroup{}

		wg.Add(100)
		for i := 0; i < 100; i++ {
			go func(wg *sync.WaitGroup) {
				hub.SendToGroup(groupId, []byte(fmt.Sprintf("groupId [%v]", groupId)))
				wg.Done()
			}(&wg)
		}

		wg.Wait()

		logrus.Info("all messages sent")
	})

	//spammer := wsclient.NewClient()
	//router.GET("/spam", func(ctx *gin.Context) {
	//	spammer.Spam()
	//})

	httpServe(router, "", "8080")
	cancel()

	<-cancelled

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
