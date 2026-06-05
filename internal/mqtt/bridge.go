package mqtt

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/hauke-cloud/iot/valve-controller/internal/metrics"
)

// TopicEntry is a single MQTT subscription derived from MQTTBridgeSpec.Topics.
// Type must be "telemetry" or "result"; other values are silently skipped.
type TopicEntry struct {
	Topic string
	Type  string // "telemetry" | "result"
	QoS   byte
}

// BridgeConfig holds all parameters needed to connect to one Tasmota MQTT bridge.
type BridgeConfig struct {
	// BridgeName is the Tasmota bridge topic prefix (e.g. "tasmota_office").
	BridgeName string
	Host       string
	Port       int32
	Username   string
	Password   string
	// ClientID suffix — full ID is "valve-controller-<hostname>-<BridgeName>".
	Hostname string
	// MaxReconnectBackoff in seconds, 0 → use 60s default.
	MaxReconnectBackoff int32
	// Topics to subscribe to. When empty the default Tasmota patterns are used.
	Topics []TopicEntry
}

// BridgeClient manages a single MQTT connection for one Tasmota bridge.
type BridgeClient struct {
	cfg        BridgeConfig
	log        *slog.Logger
	metrics    *metrics.Metrics
	dispatcher *Dispatcher
	client     pahomqtt.Client
	mu         sync.Mutex
	connected  bool

	zbStatus3Mu  sync.Mutex
	zbStatus3Chs map[string]chan ZbStatus3ValveResult // keyed by device friendly name
}

func newBridgeClient(cfg BridgeConfig, disp *Dispatcher, m *metrics.Metrics, log *slog.Logger) *BridgeClient {
	bc := &BridgeClient{
		cfg:          cfg,
		log:          log.With("bridge", cfg.BridgeName),
		metrics:      m,
		dispatcher:   disp,
		zbStatus3Chs: make(map[string]chan ZbStatus3ValveResult),
	}

	opts := pahomqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", cfg.Host, cfg.Port))
	opts.SetClientID(fmt.Sprintf("valve-controller-%s-%s", cfg.Hostname, cfg.BridgeName))
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		opts.SetPassword(cfg.Password)
	}

	maxBackoff := time.Duration(cfg.MaxReconnectBackoff) * time.Second
	if maxBackoff == 0 {
		maxBackoff = 60 * time.Second
	}
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(maxBackoff)
	opts.SetConnectRetryInterval(time.Second)
	opts.SetConnectionLostHandler(bc.onConnectionLost)
	opts.SetOnConnectHandler(bc.onConnect)
	opts.SetConnectRetry(true)

	bc.client = pahomqtt.NewClient(opts)
	return bc
}

func (bc *BridgeClient) Connect(ctx context.Context) error {
	token := bc.client.Connect()
	select {
	case <-token.Done():
		if token.Error() != nil {
			return fmt.Errorf("connect to %s:%d: %w", bc.cfg.Host, bc.cfg.Port, token.Error())
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (bc *BridgeClient) Disconnect() {
	bc.client.Disconnect(500)
	bc.metrics.MQTTConnectionState.WithLabelValues(bc.cfg.BridgeName).Set(0)
}

func (bc *BridgeClient) onConnect(c pahomqtt.Client) {
	bc.mu.Lock()
	bc.connected = true
	bc.mu.Unlock()
	bc.metrics.MQTTConnectionState.WithLabelValues(bc.cfg.BridgeName).Set(1)
	bc.log.Info("MQTT connected, subscribing to topics")
	bc.resubscribe(c)
}

func (bc *BridgeClient) onConnectionLost(_ pahomqtt.Client, err error) {
	bc.mu.Lock()
	bc.connected = false
	bc.mu.Unlock()
	bc.metrics.MQTTConnectionState.WithLabelValues(bc.cfg.BridgeName).Set(0)
	bc.log.Warn("MQTT connection lost, will reconnect", "err", err)
}

func (bc *BridgeClient) resubscribe(c pahomqtt.Client) {
	if len(bc.cfg.Topics) == 0 {
		// Fall back to the default Tasmota topic layout.
		c.Subscribe(fmt.Sprintf("tele/%s/SENSOR", bc.cfg.BridgeName), 1, bc.handleSensor)
		c.Subscribe(fmt.Sprintf("stat/%s/RESULT", bc.cfg.BridgeName), 0, bc.handleResult)
		return
	}
	for _, t := range bc.cfg.Topics {
		switch t.Type {
		case "telemetry":
			c.Subscribe(t.Topic, t.QoS, bc.handleSensor)
		case "result":
			c.Subscribe(t.Topic, t.QoS, bc.handleResult)
		default:
			bc.log.Debug("ignoring topic with unhandled type", "topic", t.Topic, "type", t.Type)
		}
	}
}

func (bc *BridgeClient) handleSensor(_ pahomqtt.Client, msg pahomqtt.Message) {
	bridge := BridgeNameFromTopic(msg.Topic())
	bc.metrics.MQTTMessagesReceived.WithLabelValues(bridge).Inc()

	events, err := ParseSensorPayload(bridge, msg.Payload())
	if err != nil {
		bc.log.Debug("failed to parse sensor payload", "err", err)
		return
	}
	for _, ev := range events {
		bc.dispatcher.Dispatch(ev)
	}
}

func (bc *BridgeClient) handleResult(_ pahomqtt.Client, msg pahomqtt.Message) {
	bridge := BridgeNameFromTopic(msg.Topic())
	bc.metrics.MQTTMessagesReceived.WithLabelValues(bridge).Inc()

	payload := msg.Payload()

	ev, err := ParseResultPayload(bridge, payload)
	if err != nil {
		bc.log.Debug("failed to parse result payload", "err", err)
		return
	}
	if ev.ZbSendDone {
		bc.log.Debug("ZbSend acknowledged by bridge")
	}

	// Route ZbStatus3 responses to any registered waiters.
	results, _ := ParseZbStatus3ValvePower(payload)
	if len(results) > 0 {
		bc.zbStatus3Mu.Lock()
		for _, r := range results {
			if ch, ok := bc.zbStatus3Chs[r.DeviceName]; ok {
				select {
				case ch <- r:
				default:
				}
			} else {
				bc.log.Debug("no ZbStatus3 waiter", "device", r.DeviceName)
			}
		}
		bc.zbStatus3Mu.Unlock()
	}
}

// SendZbSend publishes a ZbSend command to this bridge for the given device.
func (bc *BridgeClient) SendZbSend(ctx context.Context, deviceName string, power bool) error {
	payload, err := ZbSendPayload(deviceName, power)
	if err != nil {
		return fmt.Errorf("build ZbSend payload: %w", err)
	}
	topic := fmt.Sprintf("cmnd/%s/ZbSend", bc.cfg.BridgeName)
	bc.log.Debug("publishing ZbSend", "topic", topic, "payload", string(payload))
	token := bc.client.Publish(topic, 1, false, payload)
	select {
	case <-token.Done():
		return token.Error()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendZbStatus3 queries the device state via cmnd/<bridge>/ZbStatus3 and waits for the response.
func (bc *BridgeClient) SendZbStatus3(ctx context.Context, deviceName string, timeout time.Duration) (ZbStatus3ValveResult, error) {
	ch := make(chan ZbStatus3ValveResult, 1)
	bc.zbStatus3Mu.Lock()
	bc.zbStatus3Chs[deviceName] = ch
	bc.zbStatus3Mu.Unlock()
	defer func() {
		bc.zbStatus3Mu.Lock()
		delete(bc.zbStatus3Chs, deviceName)
		bc.zbStatus3Mu.Unlock()
	}()

	topic := fmt.Sprintf("cmnd/%s/ZbStatus3", bc.cfg.BridgeName)
	bc.log.Debug("publishing ZbStatus3", "topic", topic, "device", deviceName)
	token := bc.client.Publish(topic, 1, false, deviceName)
	select {
	case <-token.Done():
		if token.Error() != nil {
			return ZbStatus3ValveResult{}, fmt.Errorf("publish ZbStatus3: %w", token.Error())
		}
	case <-ctx.Done():
		return ZbStatus3ValveResult{}, ctx.Err()
	}

	select {
	case result := <-ch:
		return result, nil
	case <-time.After(timeout):
		return ZbStatus3ValveResult{}, fmt.Errorf("ZbStatus3 response timeout for %q after %s", deviceName, timeout)
	case <-ctx.Done():
		return ZbStatus3ValveResult{}, ctx.Err()
	}
}

// IsConnected reports whether the bridge is currently connected.
func (bc *BridgeClient) IsConnected() bool {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return bc.connected
}

