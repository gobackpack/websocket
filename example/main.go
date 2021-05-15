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
	router.LoadHTMLFiles("example/index.html")

	pprof.Register(router)

	router.GET("/join/:groupId", func(c *gin.Context) {
		c.HTML(200, "index.html", nil)
	})

	hub := websocket.NewHub()

	done := make(chan bool)
	cancelled := hub.ListenConnections(done)

	router.GET("/ws/:groupId", func(ctx *gin.Context) {
		groupId := ctx.Param("groupId")

		_, err := hub.EstablishConnection(ctx.Writer, ctx.Request, groupId)
		if err != nil {
			logrus.Errorf("failed to establish connection with groupId -> %s", groupId)
			return
		}
	})

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

	router.GET("/connections", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, hub.Groups)
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
