package main

import (
	"errors"
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

	hub := websocket.NewHub()

	done := make(chan bool)
	cancelled := hub.ListenConnections(done)

	router.GET("/ws/:connectionId", func(ctx *gin.Context) {
		connectionId := ctx.Param("connectionId")

		if err := hub.EstablishConnection(ctx.Writer, ctx.Request, connectionId); err != nil {
			logrus.Errorf("failed to establish connection -> %s", connectionId)
		}
	})

	router.POST("/doWork", func(ctx *gin.Context) {
		connectionId := ctx.GetHeader("connectionId")
		if strings.TrimSpace(connectionId) == "" {
			ctx.JSON(http.StatusUnauthorized, "invalid connectionId")
			return
		}

		// do some work
		// notify user about after work is done

		if err := hub.SendMessage(connectionId, []byte("hello :)")); err != nil {
			ctx.JSON(http.StatusBadRequest, "failed to send message")
			return
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
