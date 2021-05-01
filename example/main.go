package main

import (
	"errors"
	"fmt"
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

	router.GET("/join/:connectionId", func(c *gin.Context) {
		c.HTML(200, "index.html", nil)
	})

	hub := websocket.NewHub()

	done := make(chan bool)
	cancelled := hub.ListenConnections(done)

	router.GET("/ws/:connectionId", func(ctx *gin.Context) {
		connectionId := ctx.Param("connectionId")

		client, err := hub.EstablishConnection(ctx.Writer, ctx.Request, connectionId)
		if err != nil {
			logrus.Errorf("failed to establish connection -> %s", connectionId)
			return
		}

		logrus.Info("client established connection: ", client)
	})

	router.POST("/doWork", func(ctx *gin.Context) {
		groupId := ctx.GetHeader("group_id")
		if strings.TrimSpace(groupId) == "" {
			ctx.JSON(http.StatusBadRequest, "missing group_id from headers")
			return
		}

		go func() {
			for i := 0; i < 5; i++ {
				hub.SendToGroup(groupId, []byte(fmt.Sprintf("groupId [%v]", groupId)))
			}
		}()

		go func() {
			for i := 0; i < 5; i++ {
				hub.SendToAllGroups([]byte("all groups"))
			}
		}()


		connId := ctx.GetHeader("connection_id")
		if strings.TrimSpace(connId) != "" {
			go func() {
				for i := 0; i < 5; i++ {
					hub.SendToConnectionId(groupId, connId, []byte(fmt.Sprintf("groupId [%v] connectionId [%v]", groupId, connId)))
				}
			}()
		}
	})

	httpServe(router, "", "8080")

	close(done)
	<-cancelled

	logrus.Info("server exited")
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

	logrus.Warn("server exited")
}
