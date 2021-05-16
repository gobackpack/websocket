package main

import (
	"errors"
	"fmt"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/gobackpack/websocket"
	"github.com/gobackpack/websocket/example/tick"
	"github.com/gobackpack/websocket/example/trade"
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
	router.LoadHTMLFiles("example/index.html")

	pprof.Register(router)

	// serve frontend
	// this url will call /ws/:groupId (from frontend) which is going to establish ws connection
	router.GET("/join/:groupId", func(c *gin.Context) {
		c.HTML(200, "index.html", nil)
	})

	hub := websocket.NewHub()

	done := make(chan bool)
	cancelled := hub.ListenConnections(done)

	// connect client to group
	router.GET("/ws/:groupId", func(ctx *gin.Context) {
		groupId := ctx.Param("groupId")

		client, err := hub.EstablishConnection(ctx.Writer, ctx.Request, groupId)
		if err != nil {
			logrus.Errorf("failed to establish connection with groupId -> %s", groupId)
			return
		}

		client.OnMessage = func(msg []byte) error {
			for i := 0; i < 50; i++ {
				go hub.SendToOthersInGroup(groupId, client.ConnectionId, msg)
			}
			return nil
		}

		// NOTE: find your own way to return client.ConnectionId to frontend
		// client.ConnectionId is required for manual /disconnect
	})

	// disconnect client from group
	router.POST("/disconnect", func(ctx *gin.Context) {
		groupId := ctx.GetHeader("group_id")
		if strings.TrimSpace(groupId) == "" {
			ctx.JSON(http.StatusBadRequest, "missing group_id from headers")
			return
		}

		connId := ctx.GetHeader("connection_id")
		if strings.TrimSpace(connId) == "" {
			ctx.JSON(http.StatusBadRequest, "missing connection_id from headers")
			return
		}

		hub.DisconnectFromGroup(groupId, connId)
	})

	// get all groups and clients
	router.GET("/connections", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, hub.Groups)
	})

	// examples
	router.POST("/sendMessage", func(ctx *gin.Context) {
		groupId := ctx.GetHeader("group_id")
		if strings.TrimSpace(groupId) == "" {
			ctx.JSON(http.StatusBadRequest, "missing group_id from headers")
			return
		}

		wg := sync.WaitGroup{}
		wg.Add(150)

		go func() {
			for i := 0; i < 50; i++ {
				hub.SendToGroup(groupId, []byte(fmt.Sprintf("groupId [%v]", groupId)))
				wg.Done()
			}
		}()

		go func() {
			for i := 0; i < 50; i++ {
				hub.SendToAllGroups([]byte("all groups"))
				wg.Done()
			}
		}()

		connectionId := ctx.GetHeader("connection_id")
		if strings.TrimSpace(connectionId) != "" {
			go func() {
				for i := 0; i < 50; i++ {
					hub.SendToConnectionId(groupId, connectionId, []byte(fmt.Sprintf("groupId [%v] connectionId [%v]", groupId, connectionId)))
					wg.Done()
				}
			}()
		}

		wg.Wait()
	})

	// app 1
	ticker := tick.NewTicker(hub)
	router.GET("/ticks/start", func(ctx *gin.Context) {
		ticker.Start()
	})
	router.GET("/ticks/stop", func(ctx *gin.Context) {
		ticker.Stop()
	})

	// app 2
	trader := trade.NewTrader(hub)
	router.GET("/trader/start", func(ctx *gin.Context) {
		trader.Start()
	})
	router.GET("/trader/stop", func(ctx *gin.Context) {
		trader.Stop()
	})

	httpServe(router, "", "8080")

	close(done)
	<-cancelled
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
