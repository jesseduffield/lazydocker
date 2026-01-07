// Copyright 2014 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// EventsOptions to filter events
// See https://docs.docker.com/engine/api/v1.41/#operation/SystemEvents for more details.
type EventsOptions struct {
	// Show events created since this timestamp then stream new events.
	Since string

	// Show events created until this timestamp then stop streaming.
	Until string

	// Filter for events. For example:
	//  map[string][]string{"type": {"container"}, "event": {"start", "die"}}
	// will return events when container was started and stopped or killed
	//
	// Available filters:
	//  config=<string> config name or ID
	//  container=<string> container name or ID
	//  daemon=<string> daemon name or ID
	//  event=<string> event type
	//  image=<string> image name or ID
	//  label=<string> image or container label
	//  network=<string> network name or ID
	//  node=<string> node ID
	//  plugin= plugin name or ID
	//  scope= local or swarm
	//  secret=<string> secret name or ID
	//  service=<string> service name or ID
	//  type=<string> container, image, volume, network, daemon, plugin, node, service, secret or config
	//  volume=<string> volume name
	Filters map[string][]string
}

// APIEvents represents events coming from the Docker API
// The fields in the Docker API changed in API version 1.22, and
// events for more than images and containers are now fired off.
// To maintain forward and backward compatibility, go-dockerclient
// replicates the event in both the new and old format as faithfully as possible.
//
// For events that only exist in 1.22 in later, `Status` is filled in as
// `"Type:Action"` instead of just `Action` to allow for older clients to
// differentiate and not break if they rely on the pre-1.22 Status types.
//
// The transformEvent method can be consulted for more information about how
// events are translated from new/old API formats
type APIEvents struct {
	// New API Fields in 1.22
	Action string   `json:"action,omitempty"`
	Type   string   `json:"type,omitempty"`
	Actor  APIActor `json:"actor,omitempty"`

	// Old API fields for < 1.22
	Status string `json:"status,omitempty"`
	ID     string `json:"id,omitempty"`
	From   string `json:"from,omitempty"`

	// Fields in both
	Time     int64 `json:"time,omitempty"`
	TimeNano int64 `json:"timeNano,omitempty"`
}

// APIActor represents an actor that accomplishes something for an event
type APIActor struct {
	ID         string            `json:"id,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type eventMonitoringState struct {
	// `sync/atomic` expects the first word in an allocated struct to be 64-bit
	// aligned on both ARM and x86-32. See https://goo.gl/zW7dgq for more details.
	lastSeen int64
	sync.RWMutex
	sync.WaitGroup
	enabled   bool
	C         chan *APIEvents
	errC      chan error
	listeners []chan<- *APIEvents
	closeConn func()
}

const (
	maxMonitorConnRetries = 5
	retryInitialWaitTime  = 10.
)

var (
	// ErrNoListeners is the error returned when no listeners are available
	// to receive an event.
	ErrNoListeners = errors.New("no listeners present to receive event")

	// ErrListenerAlreadyExists is the error returned when the listerner already
	// exists.
	ErrListenerAlreadyExists = errors.New("listener already exists for docker events")

	// ErrTLSNotSupported is the error returned when the client does not support
	// TLS (this applies to the Windows named pipe client).
	ErrTLSNotSupported = errors.New("tls not supported by this client")

	// EOFEvent is sent when the event listener receives an EOF error.
	EOFEvent = &APIEvents{
		Type:   "EOF",
		Status: "EOF",
	}
)

// AddEventListener adds a new listener to container events in the Docker API.
//
// The parameter is a channel through which events will be sent.
func (c *Client) AddEventListener(listener chan<- *APIEvents) error {
	return c.AddEventListenerWithOptions(EventsOptions{}, listener)
}

// AddEventListener adds a new listener to container events in the Docker API.
// See https://docs.docker.com/engine/api/v1.41/#operation/SystemEvents for more details.
//
// The listener parameter is a channel through which events will be sent.
func (c *Client) AddEventListenerWithOptions(options EventsOptions, listener chan<- *APIEvents) error {
	var err error
	if !c.eventMonitor.isEnabled() {
		err = c.eventMonitor.enableEventMonitoring(c, options)
		if err != nil {
			return err
		}
	}
	return c.eventMonitor.addListener(listener)
}

// RemoveEventListener removes a listener from the monitor.
func (c *Client) RemoveEventListener(listener chan *APIEvents) error {
	err := c.eventMonitor.removeListener(listener)
	if err != nil {
		return err
	}
	if c.eventMonitor.listernersCount() == 0 {
		c.eventMonitor.disableEventMonitoring()
	}
	return nil
}

func (eventState *eventMonitoringState) addListener(listener chan<- *APIEvents) error {
	eventState.Lock()
	defer eventState.Unlock()
	if listenerExists(listener, &eventState.listeners) {
		return ErrListenerAlreadyExists
	}
	eventState.Add(1)
	eventState.listeners = append(eventState.listeners, listener)
	return nil
}

func (eventState *eventMonitoringState) removeListener(listener chan<- *APIEvents) error {
	eventState.Lock()
	defer eventState.Unlock()
	if listenerExists(listener, &eventState.listeners) {
		var newListeners []chan<- *APIEvents
		for _, l := range eventState.listeners {
			if l != listener {
				newListeners = append(newListeners, l)
			}
		}
		eventState.listeners = newListeners
		eventState.Add(-1)
	}
	return nil
}

func (eventState *eventMonitoringState) closeListeners() {
	for _, l := range eventState.listeners {
		close(l)
		eventState.Add(-1)
	}
	eventState.listeners = nil
}

func (eventState *eventMonitoringState) listernersCount() int {
	eventState.RLock()
	defer eventState.RUnlock()
	return len(eventState.listeners)
}

func listenerExists(a chan<- *APIEvents, list *[]chan<- *APIEvents) bool {
	for _, b := range *list {
		if b == a {
			return true
		}
	}
	return false
}

func (eventState *eventMonitoringState) enableEventMonitoring(c *Client, opts EventsOptions) error {
	eventState.Lock()
	defer eventState.Unlock()
	if !eventState.enabled {
		eventState.enabled = true
		atomic.StoreInt64(&eventState.lastSeen, 0)
		eventState.C = make(chan *APIEvents, 100)
		eventState.errC = make(chan error, 1)
		go eventState.monitorEvents(c, opts)
	}
	return nil
}

func (eventState *eventMonitoringState) disableEventMonitoring() {
	eventState.Lock()
	defer eventState.Unlock()

	eventState.closeListeners()

	eventState.Wait()

	if eventState.enabled {
		eventState.enabled = false
		close(eventState.C)
		close(eventState.errC)

		if eventState.closeConn != nil {
			eventState.closeConn()
			eventState.closeConn = nil
		}
	}
}

func (eventState *eventMonitoringState) monitorEvents(c *Client, opts EventsOptions) {
	const (
		noListenersTimeout  = 5 * time.Second
		noListenersInterval = 10 * time.Millisecond
		noListenersMaxTries = noListenersTimeout / noListenersInterval
	)

	var err error
	for i := time.Duration(0); i < noListenersMaxTries && eventState.noListeners(); i++ {
		time.Sleep(10 * time.Millisecond)
	}

	if eventState.noListeners() {
		// terminate if no listener is available after 5 seconds.
		// Prevents goroutine leak when RemoveEventListener is called
		// right after AddEventListener.
		eventState.disableEventMonitoring()
		return
	}

	if err = eventState.connectWithRetry(c, opts); err != nil {
		// terminate if connect failed
		eventState.disableEventMonitoring()
		return
	}
	for eventState.isEnabled() {
		timeout := time.After(100 * time.Millisecond)
		select {
		case ev, ok := <-eventState.C:
			if !ok {
				return
			}
			if ev == EOFEvent {
				go eventState.disableEventMonitoring()
				return
			}
			go func(ev *APIEvents) {
				eventState.updateLastSeen(ev)
				eventState.sendEvent(ev)
			}(ev)
		case err = <-eventState.errC:
			if errors.Is(err, ErrNoListeners) {
				eventState.disableEventMonitoring()
				return
			} else if err != nil {
				defer func() { go eventState.monitorEvents(c, opts) }()
				return
			}
		case <-timeout:
			continue
		}
	}
}

func (eventState *eventMonitoringState) connectWithRetry(c *Client, opts EventsOptions) error {
	var retries int
	eventState.RLock()
	eventChan := eventState.C
	errChan := eventState.errC
	eventState.RUnlock()
	closeConn, err := c.eventHijack(opts, atomic.LoadInt64(&eventState.lastSeen), eventChan, errChan)
	for ; err != nil && retries < maxMonitorConnRetries; retries++ {
		waitTime := int64(retryInitialWaitTime * math.Pow(2, float64(retries)))
		time.Sleep(time.Duration(waitTime) * time.Millisecond)
		eventState.RLock()
		eventChan = eventState.C
		errChan = eventState.errC
		eventState.RUnlock()
		closeConn, err = c.eventHijack(opts, atomic.LoadInt64(&eventState.lastSeen), eventChan, errChan)
	}
	eventState.Lock()
	defer eventState.Unlock()
	eventState.closeConn = closeConn
	return err
}

func (eventState *eventMonitoringState) noListeners() bool {
	eventState.RLock()
	defer eventState.RUnlock()
	return len(eventState.listeners) == 0
}

func (eventState *eventMonitoringState) isEnabled() bool {
	eventState.RLock()
	defer eventState.RUnlock()
	return eventState.enabled
}

func (eventState *eventMonitoringState) sendEvent(event *APIEvents) {
	eventState.RLock()
	defer eventState.RUnlock()
	eventState.Add(1)
	defer eventState.Done()
	if eventState.enabled {
		if len(eventState.listeners) == 0 {
			eventState.errC <- ErrNoListeners
			return
		}

		for _, listener := range eventState.listeners {
			select {
			case listener <- event:
			default:
			}
		}
	}
}

func (eventState *eventMonitoringState) updateLastSeen(e *APIEvents) {
	eventState.Lock()
	defer eventState.Unlock()
	if atomic.LoadInt64(&eventState.lastSeen) < e.Time {
		atomic.StoreInt64(&eventState.lastSeen, e.Time)
	}
}

func (c *Client) eventHijack(opts EventsOptions, startTime int64, eventChan chan *APIEvents, errChan chan error) (closeConn func(), err error) {
	// on reconnect override initial Since with last event seen time
	if startTime != 0 {
		opts.Since = strconv.FormatInt(startTime, 10)
	}
	uri := "/events?" + queryString(opts)
	protocol := c.endpointURL.Scheme
	address := c.endpointURL.Path
	if protocol != "unix" && protocol != "npipe" {
		protocol = "tcp"
		address = c.endpointURL.Host
	}
	var dial net.Conn
	if c.TLSConfig == nil {
		dial, err = c.Dialer.Dial(protocol, address)
	} else {
		netDialer, ok := c.Dialer.(*net.Dialer)
		if !ok {
			return nil, ErrTLSNotSupported
		}
		dial, err = tlsDialWithDialer(netDialer, protocol, address, c.TLSConfig)
	}
	if err != nil {
		return nil, err
	}
	//lint:ignore SA1019 the alternative doesn't quite work, so keep using the deprecated thing.
	conn := httputil.NewClientConn(dial, nil)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	res, err := conn.Do(req)
	if err != nil {
		return nil, err
	}

	keepRunning := int32(1)
	//lint:ignore SA1019 the alternative doesn't quite work, so keep using the deprecated thing.
	go func(res *http.Response, conn *httputil.ClientConn) {
		defer conn.Close()
		defer res.Body.Close()
		decoder := json.NewDecoder(res.Body)
		for atomic.LoadInt32(&keepRunning) == 1 {
			var event APIEvents
			if err := decoder.Decode(&event); err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
					c.eventMonitor.RLock()
					if c.eventMonitor.enabled && c.eventMonitor.C == eventChan {
						// Signal that we're exiting.
						eventChan <- EOFEvent
					}
					c.eventMonitor.RUnlock()
					break
				}
				errChan <- err
			}
			if event.Time == 0 {
				continue
			}
			transformEvent(&event)
			c.eventMonitor.RLock()
			if c.eventMonitor.enabled && c.eventMonitor.C == eventChan {
				eventChan <- &event
			}
			c.eventMonitor.RUnlock()
		}
	}(res, conn)
	return func() {
		atomic.StoreInt32(&keepRunning, 0)
	}, nil
}

// transformEvent takes an event and determines what version it is from
// then populates both versions of the event
func transformEvent(event *APIEvents) {
	// if event version is <= 1.21 there will be no Action and no Type
	if event.Action == "" && event.Type == "" {
		event.Action = event.Status
		event.Actor.ID = event.ID
		event.Actor.Attributes = map[string]string{}
		switch event.Status {
		case "delete", "import", "pull", "push", "tag", "untag":
			event.Type = "image"
		default:
			event.Type = "container"
			if event.From != "" {
				event.Actor.Attributes["image"] = event.From
			}
		}
	} else {
		if event.Status == "" {
			if event.Type == "image" || event.Type == "container" {
				event.Status = event.Action
			} else {
				// Because just the Status has been overloaded with different Types
				// if an event is not for an image or a container, we prepend the type
				// to avoid problems for people relying on actions being only for
				// images and containers
				event.Status = event.Type + ":" + event.Action
			}
		}
		if event.ID == "" {
			event.ID = event.Actor.ID
		}
		if event.From == "" {
			event.From = event.Actor.Attributes["image"]
		}
	}
}
