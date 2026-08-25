package events

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PastureStack/load-balancer-controller/internal/rancherclient/v2"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

const MaxWait = time.Duration(time.Second * 10)

// EventHandler Defines the function "interface" that handlers must conform to.
type EventHandler func(*Event, *client.RancherClient) error

type EventRouter struct {
	name          string
	priority      int
	apiURL        string
	accessKey     string
	secretKey     string
	apiClient     *client.RancherClient
	subscribeURL  string
	eventHandlers map[string]EventHandler
	workerCount   int
	eventStream   *websocket.Conn
	resourceName  string
	pingConfig    PingConfig
}

func NewEventRouter(name string, priority int, apiURL string, accessKey string, secretKey string,
	apiClient *client.RancherClient, eventHandlers map[string]EventHandler, resourceName string, workerCount int,
	pingConfig PingConfig) (*EventRouter, error) {

	if apiClient == nil {
		var err error
		apiClient, err = client.NewRancherClient(&client.ClientOpts{
			Timeout:   time.Second * time.Duration(defaultTimeout()),
			Url:       apiURL,
			AccessKey: accessKey,
			SecretKey: secretKey,
		})
		if err != nil {
			return nil, err
		}
	}

	// TODO Get subscribe collection URL from API instead of hard coding.
	subscribe, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("parse event API URL: %w", err)
	}
	switch subscribe.Scheme {
	case "http":
		subscribe.Scheme = "ws"
	case "https":
		subscribe.Scheme = "wss"
	default:
		return nil, fmt.Errorf("unsupported event API URL scheme %q", subscribe.Scheme)
	}
	subscribe.Path = strings.TrimSuffix(subscribe.Path, "/") + "/subscribe"
	subscribe.RawQuery = ""
	subscribe.Fragment = ""
	subscribeURL := subscribe.String()

	return &EventRouter{
		name:          name,
		priority:      priority,
		apiURL:        apiURL,
		accessKey:     accessKey,
		secretKey:     secretKey,
		apiClient:     apiClient,
		subscribeURL:  subscribeURL,
		eventHandlers: eventHandlers,
		workerCount:   workerCount,
		resourceName:  resourceName,
		pingConfig:    pingConfig,
	}, nil
}

// The difference between Start and StartWithoutCreate is a matter of making this event router
// more generally usable. The Start implementation creates
// the necessary ExternalHandler upon start up. This router has been refactor to
// be used in situations where creating an externalHandler is not desired.
// This allows the router to be used for Agent connections and for ExternalHandlers
// that are created outside of this router.

func (router *EventRouter) Start(ready chan<- bool) error {
	err := router.createExternalHandler()
	if err != nil {
		return err
	}
	eventSuffix := ";handler=" + router.name
	wp := SkippingWorkerPool(router.workerCount, resourceIDLocker)
	return router.run(wp, ready, eventSuffix)
}

func (router *EventRouter) StartWithoutCreate(ready chan<- bool) error {
	wp := SkippingWorkerPool(router.workerCount, resourceIDLocker)
	return router.run(wp, ready, "")
}

func (router *EventRouter) RunWithWorkerPool(wp WorkerPool) error {
	return router.run(wp, nil, "")
}

func (router *EventRouter) run(wp WorkerPool, ready chan<- bool, eventSuffix string) (err error) {

	log.WithFields(log.Fields{
		"workerCount": router.workerCount,
	}).Info("Initializing event router")

	handlers := map[string]EventHandler{}

	if pingHandler, ok := router.eventHandlers["ping"]; ok {
		// Ping doesnt need registered in the POST and ping events don't have the handler suffix.
		//If we start handling other non-suffix events, we might consider improving this.
		handlers["ping"] = pingHandler
	}

	subscribeParams := url.Values{}
	for event, handler := range router.eventHandlers {
		fullEventKey := event + eventSuffix
		subscribeParams.Add("eventNames", fullEventKey)
		handlers[fullEventKey] = handler
	}

	eventStream, err := router.subscribeToEvents(router.subscribeURL, router.accessKey, router.secretKey, subscribeParams)
	if err != nil {
		return err
	}
	log.Info("Connection established")
	router.eventStream = eventStream
	defer router.Stop()

	if ready != nil {
		ready <- true
	}

	ph := newPongHandler(router)
	defer ph.stop()
	router.eventStream.SetPongHandler(ph.handle)
	go router.sendWebsocketPings()

	for {
		_, message, err := router.eventStream.ReadMessage()
		if err != nil {
			// Error here means the connection is closed. It's normal, so just return.
			return nil
		}

		message = bytes.TrimSpace(message)
		if len(message) == 0 {
			continue
		}

		event := &Event{}
		err = json.Unmarshal(message, &event)
		if err != nil {
			log.WithFields(log.Fields{
				"messageBytes": len(message),
			}).Warnf("Error parsing message: %s", err)
			continue
		}
		wp.HandleWork(event, handlers, router.apiClient)
	}
}

func (router *EventRouter) Stop() {
	router.eventStream.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	router.eventStream.Close()
}

func (router *EventRouter) subscribeToEvents(subscribeURL string, accessKey string, secretKey string, data url.Values) (*websocket.Conn, error) {
	dialer := &websocket.Dialer{}
	headers := http.Header{}
	headers.Add("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(accessKey+":"+secretKey)))
	endpoint, err := url.Parse(subscribeURL)
	if err != nil {
		return nil, fmt.Errorf("parse event subscription URL: %w", err)
	}
	query := endpoint.Query()
	for key, values := range data {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	endpoint.RawQuery = query.Encode()
	ws, resp, err := dialer.Dial(endpoint.String(), headers)

	if err != nil {
		log.WithFields(log.Fields{
			"subscribeEndpoint": endpoint.Scheme + "://" + endpoint.Host + endpoint.Path,
		}).Errorf("Error subscribing to events: %s", err)
		if resp != nil {
			log.WithFields(log.Fields{
				"status":     resp.Status,
				"statusCode": resp.StatusCode,
			}).Error("Got error response")
			if resp.Body != nil {
				defer resp.Body.Close()
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			}
		}
		if ws != nil {
			ws.Close()
		}
		return nil, err
	}
	return ws, nil
}

func (router *EventRouter) GetWebSocketConn() *websocket.Conn {
	return router.eventStream
}

func defaultTimeout() int {
	defaultTimeout, _ := strconv.Atoi(os.Getenv("RANCHER_CLIENT_TIMEOUT"))
	if defaultTimeout == 0 {
		defaultTimeout = 30
	}
	return defaultTimeout
}
