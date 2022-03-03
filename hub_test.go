package websocket_test

import (
	"context"
	"github.com/gobackpack/websocket"
	"github.com/sirupsen/logrus"
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkHub_SendToGroup(b *testing.B) {
	hub := websocket.NewHub()

	hubCtx, hubCancel := context.WithCancel(context.Background())
	cancelled := hub.ListenConnections(hubCtx)

	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := hub.EstablishConnection(w, r, "1", "1")
			if err != nil {
				logrus.Errorf("failed to establish connection with groupId -> %s", "1")
				return
			}
		}))

	defer ts.Close()

	//for n := 0; n < b.N; n++ {
	//	go hub.SendToGroup("1", []byte("123456789"))
	//}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			hub.SendToGroup("1", []byte("123456789"))
		}
	})

	hubCancel()
	<-cancelled
}

func BenchmarkHub_SendToAllGroups(b *testing.B) {
	hub := websocket.NewHub()

	hubCtx, hubCancel := context.WithCancel(context.Background())
	cancelled := hub.ListenConnections(hubCtx)

	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := hub.EstablishConnection(w, r, "1", "1")
			if err != nil {
				b.Log(err)
			}
		}))

	defer ts.Close()

	for n := 0; n < b.N; n++ {
		go hub.SendToAllGroups([]byte("123456789"))
	}

	hubCancel()
	<-cancelled
}

func BenchmarkHub_SendToConnectionId(b *testing.B) {
	hub := websocket.NewHub()

	hubCtx, hubCancel := context.WithCancel(context.Background())
	cancelled := hub.ListenConnections(hubCtx)

	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := hub.EstablishConnection(w, r, "1", "1")
			if err != nil {
				b.Log(err)
			}
		}))

	defer ts.Close()

	for n := 0; n < b.N; n++ {
		go hub.SendToConnectionId("1", "1", []byte("123456789"))
	}

	hubCancel()
	<-cancelled
}

func BenchmarkHub_SendToOthersInGroup(b *testing.B) {
	hub := websocket.NewHub()

	hubCtx, hubCancel := context.WithCancel(context.Background())
	cancelled := hub.ListenConnections(hubCtx)

	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := hub.EstablishConnection(w, r, "1", "1")
			if err != nil {
				b.Log(err)
			}
		}))

	defer ts.Close()

	for n := 0; n < b.N; n++ {
		go hub.SendToOthersInGroup("1", "1", []byte("123456789"))
	}

	hubCancel()
	<-cancelled
}

func BenchmarkHub_EstablishConnection(b *testing.B) {
	hub := websocket.NewHub()

	hubCtx, hubCancel := context.WithCancel(context.Background())
	cancelled := hub.ListenConnections(hubCtx)

	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for n := 0; n < b.N; n++ {
				_, err := hub.EstablishConnection(w, r, "1", "1")
				if err != nil {
					b.Log(err)
				}
			}
		}))

	defer ts.Close()

	hubCancel()
	<-cancelled
}
