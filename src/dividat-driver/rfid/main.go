package rfid

/* Service for RFID tag touch-on events and listing connected readers.

The purpose of this service is to notify subscribers of any RFID tags read by
readers available to the host machine. The only information extracted from tags
is their UID.

In order to subscribe to RFID events, a client can open a WebSocket
connection to

    /rfid

and will receive messages in case a new tag is read or the list of available
readers changes.

In addition, the current list is retrievable through simple GET request to

    /rfid/readers

The intended range of supported readers and tags includes all systems
communicating according to ISO 7816-4, granted that the readers offer PC/SC
support.

The range of supported operating systems includes Windows and macOS, which come
with bundled PC/SC services, as well as Linux systems running the libpcsclite
service (pcscd).

For details on the implementation and strategy of working with readers, see
`pcsc.go`. The detection loop is only active if there are subscribers.

*/

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/cskr/pubsub"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

const Topic = "rfid-tokens"

// RFID handle

type Handle struct {
	broker *pubsub.PubSub

	ctx context.Context

	cancelPolling   context.CancelFunc
	subscriberCount int
	knownReaders    []string

	log *logrus.Entry
}

func NewHandle(ctx context.Context, log *logrus.Entry) *Handle {
	handle := Handle{
		broker:       pubsub.New(2),
		ctx:          ctx,
		log:          log,
		knownReaders: []string{},
	}

	// Clean up
	go func() {
		<-ctx.Done()
		handle.broker.Shutdown()
	}()

	return &handle
}

func (handle *Handle) DeregisterSubscriber() {
	handle.subscriberCount--

	if handle.subscriberCount == 0 {
		handle.cancelPolling()
		handle.cancelPolling = nil
	}
}

func (handle *Handle) EnsureSmartCardPolling() {
	if handle.cancelPolling == nil {
		ctx, cancel := context.WithCancel(handle.ctx)
		handle.cancelPolling = cancel
		emitToken := func(token string) {
			handle.broker.TryPub(Message{Identified: &token}, Topic)
		}
		// Start a polling routine and push any tokens it produces onto the bus
		go pollSmartCard(
			ctx,
			handle.log,
			emitToken,
			func(knownReaders []string) {
				handle.knownReaders = knownReaders
				handle.broker.TryPub(Message{ReadersChanged: &knownReaders}, Topic)
			},
		)
		// Also re-fetch the current token from pcscd every second and emit it
		go pollCurrentTokens(ctx, handle.log, emitToken)
	}

	handle.subscriberCount++
}

// WEBSOCKET PROTOCOL

// Message that can be sent to Play
type Message struct {
	Identified     *string
	ReadersChanged *[]string
}

func (message *Message) MarshalJSON() ([]byte, error) {
	if message.Identified != nil {
		return json.Marshal(&struct {
			Type  string `json:"type"`
			Token string `json:"token"`
		}{
			Type:  "Identified",
			Token: *message.Identified,
		})
	} else if message.ReadersChanged != nil {
		return json.Marshal(&struct {
			Type    string   `json:"type"`
			Readers []string `json:"readers"`
		}{
			Type:    "ReadersChanged",
			Readers: *message.ReadersChanged,
		})
	}

	return nil, errors.New("could not marshal message")
}

func (handle *Handle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" && r.URL.Path == "/rfid/readers" {
		handle.ServerReaderList(w, r)
	} else if r.URL.Path == "/rfid" || r.URL.Path == "/rfid/" {
		handle.StreamEvents(w, r)
	} else {
		http.NotFound(w, r)
	}
}

func (handle *Handle) ServerReaderList(w http.ResponseWriter, r *http.Request) {
	readersJson, _ := json.Marshal(&struct {
		Readers []string `json:"readers"`
	}{
		Readers: handle.knownReaders,
	})
	w.Header().Set("Content-Type", "application/json")
	w.Write(readersJson)
}

func (handle *Handle) StreamEvents(w http.ResponseWriter, r *http.Request) {
	handle.EnsureSmartCardPolling()

	// Set up logger
	var log = handle.log.WithFields(logrus.Fields{
		"clientAddress": r.RemoteAddr,
		"userAgent":     r.UserAgent(),
	})

	// Upgrade to WebSocket
	conn, err := webSocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithError(err).Error("Could not upgrade connection to WebSocket.")
		http.Error(w, "WebSocket upgrade error", http.StatusBadRequest)
		return
	}

	log.Info("WebSocket connection opened")

	// Create a mutex for writing to WebSocket (connection supports only one concurrent reader and one concurrent writer (https://godoc.org/github.com/gorilla/websocket#hdr-Concurrency))
	writeMutex := sync.Mutex{}

	// Create a context for this WebSocket connection
	ctx, cancel := context.WithCancel(context.Background())

	// Subscribe to tokens and proxy received messages
	send := func(message Message) error {
		writeMutex.Lock()
		conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
		err := conn.WriteJSON(&message)
		writeMutex.Unlock()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.WithError(err).Error("WebSocket error")
			}
			return err
		}
		return nil
	}
	rx := handle.broker.Sub(Topic)
	go rx_data_loop(ctx, rx, send)

	// Helper function to close the connection
	close := func() {
		handle.broker.Unsub(rx)

		// Cancel the context
		cancel()

		// Close websocket connection
		conn.Close()

		handle.DeregisterSubscriber()

		log.Info("WebSocket connection closed")
	}

	// Main loop for the WebSocket connection
	go func() {
		defer close()
		for {

			_, _, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					log.WithError(err).Error("WebSocket error")
				}
				return
			}

		}
	}()
}

func rx_data_loop(ctx context.Context, rx chan interface{}, send func(Message) error) {
	var err error
	for {
		select {
		case <-ctx.Done():
			return

		case i := <-rx:
			data, ok := i.(Message)
			if ok {
				err = send(data)
			}
		}

		if err != nil {
			return
		}
	}
}

// Helper to upgrade http to WebSocket
var webSocketUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}
