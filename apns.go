package apns

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"math/rand"
	"net"
	"time"
)

// MaxPushNotificationBytes is the maximum number of bytes permitted in a payload
// sent to the APNs
const MaxPushNotificationBytes = 2048

// Constants related to the payload fields and their lengths.
const (
	deviceTokenItemID            = 1
	payloadItemID                = 2
	notificationIdentifierItemID = 3
	expirationDateItemID         = 4
	priorityItemID               = 5
	deviceTokenLength            = 32
	notificationIdentifierLength = 4
	expirationDateLength         = 4
	priorityLength               = 1
)

// initialized in init()
var apnsErrors map[int]string

// Push commands always start with command value 2.
const pushCommandValue = 2

// ErrClientNotConnected indicates the user need to call Connect() on the
// Client, due to never having connected or due to the connection erroring out.
var ErrClientNotConnected = errors.New("Client not connected to APNs")

var recentNotifications = make([]*Notification, 0, 50)

// Notification represents a push notification for a specific iOS device
type Notification struct {
	Alert       string      `json:"alert"`
	Badge       int16       `json:"badge"`
	Sound       string      `json:"sound"`
	AppData     interface{} `json:"-"`
	deviceToken string      `json:"-"`
	identifier  int32       `json:"-"`
	expiry      uint32      `json:"-"`
	priority    uint8       `json:"-"`
}

const (
	processingErrorID    = 1
	missingDeviceTokenID = 2
	missingTopicID       = 3
	missingPayloadID     = 4
	invalidTokenSizeID   = 5
	invalidTopicSizeID   = 6
	invalidPayloadSizeID = 7
	invalidTokenID       = 8
	shutdownID           = 9
	noErrorID            = 10
)

func init() {
	apnsErrors = make(map[int]string)
	apnsErrors[0] = "No errors encountered"
	apnsErrors[processingErrorID] = "Processing error"
	apnsErrors[missingDeviceTokenID] = "Missing device token"
	apnsErrors[missingTopicID] = "Missing topic"
	apnsErrors[missingPayloadID] = "Missing payload"
	apnsErrors[invalidTokenSizeID] = "Invalid token size"
	apnsErrors[invalidTopicSizeID] = "Invalid topic size"
	apnsErrors[invalidPayloadSizeID] = "Invalid payload size"
	apnsErrors[invalidTokenID] = "Invalid token"
	apnsErrors[shutdownID] = "Shutdown"
	apnsErrors[noErrorID] = "None (unknown)"
}

// NewNotification creates an APNs notification
func NewNotification(devToken string) *Notification {
	n := &Notification{deviceToken: devToken}
	n.identifier = rand.New(rand.NewSource(time.Now().UnixNano())).Int31()
	return n
}

// ToBytes returns the Notification as a slice of bytes for transfer to APNs
func (n *Notification) ToBytes() []byte {
	return nil
}

// Client is the broker between an APNs provider and the gateway
type Client struct {
	Gateway             string
	CertificateFile     string
	KeyFile             string
	conn                *tls.Conn
	IsConnected         bool
	notificationChan    chan *Notification
	invalidTokenHandler func(string)
}

// NewClient initializes a Client struct for you
func NewClient(gateway, certFile, keyFile string, invalidTokenHandler func(string)) *Client {
	c := Client{Gateway: gateway, CertificateFile: certFile, KeyFile: keyFile}
	c.notificationChan = make(chan *Notification, 4096) // 4096 oughtta be enough for anybody
	c.invalidTokenHandler = invalidTokenHandler
	return &c
}

func (c *Client) connect() error {
	c.IsConnected = false
	cert, err := tls.LoadX509KeyPair(c.CertificateFile, c.KeyFile)
	if err != nil {
		return err
	}

	host, _, err := net.SplitHostPort(c.Gateway)
	conf := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ServerName:   host,
	}

	c.conn, err = tls.Dial("tcp", c.Gateway, conf)
	if err != nil {
		return err
	}
	c.IsConnected = true
	return nil
}

// Connect establishes a connection to the APNs gateway
func (c *Client) Connect() error {
	err := c.connect()
	if err != nil {
		return err
	}

	// start a goroutine to read this connection for any errors messages
	go c.readErrors()

	// start a goroutine to process the queue of notifications
	go c.processQueue()

	return nil
}

func (c *Client) reconnect() {
	log.Print("Attempting to reconnect to APNs")
	maxSleepTime := 60 * time.Second
	// attempt to reconnect. sleeping (2 * attempt) seconds between each retry, with a max of 60 seconds
	for attempt := 0; ; attempt++ {
		sleepTime := time.Duration(attempt) * 2 * time.Second
		if sleepTime > maxSleepTime {
			time.Sleep(maxSleepTime)
		} else {
			time.Sleep(sleepTime)
		}
		err := c.connect()
		if err == nil {
			break
		}
	}

	go c.readErrors()
	go c.processQueue()
}

// Send queues a notification for sending to the APNs
func (c *Client) Send(n *Notification) {
	// if the channel is full, discard older messages, then queue the new one
	// if we don't discard the older messages, goroutine will get locked trying
	// to put a new notification on
	if len(c.notificationChan) == cap(c.notificationChan) {
		<-c.notificationChan
	}
	c.notificationChan <- n

	// we want to keep a cache of the last 25 or 50 messages, in case we get an error
	// from Apple
	if len(recentNotifications) >= 50 {
		recentNotifications = recentNotifications[25:]
	}
	recentNotifications = append(recentNotifications, n)
}

func (c *Client) readErrors() {
	readBuf := make([]byte, 6, 6)
	_, err := c.conn.Read(readBuf)
	// the connection should be closed anytime data is received or an error occurs
	// see apple docs for details
	c.IsConnected = false
	defer c.conn.Close()
	if err != nil {
		log.Printf("Error while reading APNs socket - %v", err)
		return
	}
	var id int32
	binary.Read(bytes.NewReader(readBuf[2:]), binary.BigEndian, &id)

	// if this is an invalid device token error, notify the user
	errID := int(readBuf[1])
	if errID == invalidTokenID {
		rn := recentNotifications // store a different pointer in case the slice gets changed
		for i := len(rn) - 1; i >= 0; i-- {
			n := rn[i]
			if n.identifier == id {
				c.invalidTokenHandler(n.deviceToken)
			}
		}
	} else {
		log.Printf("Error received for notification %d - %v", id, apnsErrors[errID])
	}
}

func (c *Client) processQueue() {
	for n := range c.notificationChan {
		if !c.IsConnected {
			// put the notification back in the queue and reconnect this client
			c.notificationChan <- n
			go c.reconnect()
			return // we'll get restarted after the client reconnects
		}

		token, err := hex.DecodeString(n.deviceToken)
		if err != nil {
			log.Printf("Error decoding APNs notification token - %v", err)
			continue
		}

		payload := make(map[string]interface{})
		payload["aps"] = n
		if n.AppData != nil {
			payload["app_data"] = n.AppData
		}
		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			log.Printf("Error marshaling payload to JSON - %v", err)
			continue
		}

		if len(jsonBytes) > MaxPushNotificationBytes {
			log.Printf("Notification is larger than the byte limit (%d). Skipping.", MaxPushNotificationBytes)
			continue
		}

		buf := &bytes.Buffer{}
		binary.Write(buf, binary.BigEndian, uint8(deviceTokenItemID))
		binary.Write(buf, binary.BigEndian, uint16(deviceTokenLength))
		binary.Write(buf, binary.BigEndian, token)
		binary.Write(buf, binary.BigEndian, uint8(payloadItemID))
		binary.Write(buf, binary.BigEndian, uint16(len(jsonBytes)))
		binary.Write(buf, binary.BigEndian, jsonBytes)
		binary.Write(buf, binary.BigEndian, uint8(notificationIdentifierItemID))
		binary.Write(buf, binary.BigEndian, uint16(notificationIdentifierLength))
		binary.Write(buf, binary.BigEndian, n.identifier)
		binary.Write(buf, binary.BigEndian, uint8(expirationDateItemID))
		binary.Write(buf, binary.BigEndian, uint16(expirationDateLength))
		binary.Write(buf, binary.BigEndian, n.expiry)
		binary.Write(buf, binary.BigEndian, uint8(priorityItemID))
		binary.Write(buf, binary.BigEndian, uint16(priorityLength))
		binary.Write(buf, binary.BigEndian, n.priority)

		fullBuf := &bytes.Buffer{}
		binary.Write(fullBuf, binary.BigEndian, uint8(pushCommandValue))
		binary.Write(fullBuf, binary.BigEndian, uint32(buf.Len()))
		binary.Write(fullBuf, binary.BigEndian, buf.Bytes())

		written, err := c.conn.Write(fullBuf.Bytes())
		if err != nil {
			log.Printf("Error writing notification %v to APNs - %v", n, err)
			c.conn.Close()
			c.IsConnected = false
			// requeue the notification and try again later
			c.notificationChan <- n
			go c.reconnect()
			return // we'll get restarted after the client reconnects
		}
		if written != fullBuf.Len() {
			log.Printf("Bytes written didn't equal the bytes in the buffer - w: %d, size: %d",
				written,
				fullBuf.Len())
			c.conn.Close()
			c.IsConnected = false
			c.notificationChan <- n
			go c.reconnect()
			return
		}
	}
}
