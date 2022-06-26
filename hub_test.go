package websocket_test

import (
	"context"
	"github.com/gobackpack/websocket"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockConn struct{}

func (conn *mockConn) ReadMessage() (int, []byte, error) {
	return 0, nil, nil
}
func (conn *mockConn) WriteMessage(data []byte) error {
	return nil
}
func (conn *mockConn) Close() error {
	return nil
}

func TestNewHub(t *testing.T) {
	hub := websocket.NewHub()

	assert.NotNil(t, hub)
	assert.NotNil(t, hub.Groups)
	assert.Equal(t, 0, len(hub.Groups))
}

func TestHub_EstablishConnection(t *testing.T) {
	hub := websocket.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	finished := hub.ListenForConnections(ctx)

	mConn := &mockConn{}
	client, err := hub.EstablishConnection(mConn, "1", "1")
	assert.NoError(t, err)
	assert.NotNil(t, client)

	assert.Equal(t, 1, len(hub.Groups))
	group := hub.Groups[0]
	assert.NotNil(t, group)
	assert.Equal(t, "1", group.Id)

	assert.Equal(t, 1, len(group.Clients))
	cl := group.Clients[0]
	assert.NotNil(t, cl)
	assert.Equal(t, "1", cl.ConnectionId)

	cancel()
	<-finished
}

func BenchmarkHub_SendToGroup(b *testing.B) {
	hub := websocket.NewHub()

	hubCtx, hubCancel := context.WithCancel(context.Background())
	cancelled := hub.ListenForConnections(hubCtx)

	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := websocket.NewGorillaConnectionAdapter(w, r)
			if err != nil {
				logrus.Errorf("failed to upgrade connection: %s", err)
				return
			}
			_, err = hub.EstablishConnection(conn, "1", "1")
			if err != nil {
				logrus.Errorf("failed to establish connection with groupId -> %s", "1")
				return
			}
		}))

	defer ts.Close()

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
	cancelled := hub.ListenForConnections(hubCtx)

	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := websocket.NewGorillaConnectionAdapter(w, r)
			if err != nil {
				logrus.Errorf("failed to upgrade connection: %s", err)
				return
			}
			_, err = hub.EstablishConnection(conn, "1", "1")
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
	cancelled := hub.ListenForConnections(hubCtx)

	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := websocket.NewGorillaConnectionAdapter(w, r)
			if err != nil {
				logrus.Errorf("failed to upgrade connection: %s", err)
				return
			}
			_, err = hub.EstablishConnection(conn, "1", "1")
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
	cancelled := hub.ListenForConnections(hubCtx)

	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := websocket.NewGorillaConnectionAdapter(w, r)
			if err != nil {
				logrus.Errorf("failed to upgrade connection: %s", err)
				return
			}
			_, err = hub.EstablishConnection(conn, "1", "1")
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
	cancelled := hub.ListenForConnections(hubCtx)

	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for n := 0; n < b.N; n++ {
				conn, err := websocket.NewGorillaConnectionAdapter(w, r)
				if err != nil {
					logrus.Errorf("failed to upgrade connection: %s", err)
					return
				}
				_, err = hub.EstablishConnection(conn, "1", "1")
				if err != nil {
					b.Log(err)
				}
			}
		}))

	defer ts.Close()

	hubCancel()
	<-cancelled
}
