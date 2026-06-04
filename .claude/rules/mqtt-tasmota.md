# MQTT / Tasmota / Zigbee Rules

## Topic Structure

Tasmota bridges Zigbee-to-MQTT using a predictable topic hierarchy:

```
tele/<bridge>/SENSOR   → live readings pushed when a device reports (ZbReceived format)
stat/<bridge>/RESULT   → responses to ZbSend commands AND periodic ZbStatus poll results
cmnd/<bridge>/ZbSend   → send a command to a Zigbee device
cmnd/<bridge>/ZbStatus → request status of one or all devices (bridge responds on RESULT)
```

Multiple bridges are in use. Subscribe with `+` wildcard (`tele/+/SENSOR`, `stat/+/RESULT`) and extract the bridge name from the topic path.

### stat/+/RESULT — two distinct message types

The RESULT topic carries two formats; always check the top-level key:

**ZbStatus1** — device list only (name + address):
```json
{"ZbStatus1":[{"Device":"0xDA76","Name":"valve-center"},{"Device":"0x0B6E","Name":"valve-left"}]}
```

**ZbStatus3** — full device state (sensor values at the top level of the device object):
```json
{"ZbStatus3":[{
  "Device": "0x763B",
  "Name": "room-livingroom",
  "ModelId": "lumi.weather",
  "Manufacturer": "LUMI",
  "Temperature": 27.5,
  "Pressure": 1020,
  "Humidity": 39.72,
  "Reachable": true,
  "BatteryPercentage": 92,
  "LinkQuality": 167
}]}
```

Note: ZbStatus3 sensor fields are **top-level** inside the device object — there is no `ZbReceived` wrapper here. ZbReceived only appears on `tele/<bridge>/SENSOR` for spontaneous reports.

### tele/+/SENSOR — spontaneous device reports

```json
{"ZbReceived":{"0x1234":{
  "Device": "0x1234",
  "Name": "valve-kitchen",
  "Endpoint": 1,
  "LinkQuality": 78,
  "StateValve": 0,
  "BatteryPercentage": 82
}}}
```

## Naming Conventions

- Broker: `mqtt.home.hauke.cloud:1883` (no TLS, no auth on the local network).
- Tasmota bridge topic prefix: configured via env `MQTT_BRIDGE_TOPIC` (e.g. `tasmota/bridge`). The bridge name is the single path segment between `tele/` and `/SENSOR`.
- Device friendly names in Tasmota must match the `metadata.name` of the corresponding Kubernetes CR.
- MQTT client ID: `valve-controller-<hostname>` — unique per pod replica.

## CLI Tool

Use the HiveMQ MQTT CLI via podman for any manual probing or debugging:

```bash
# Subscribe — wildcards are fine
podman run -it --rm docker.io/hivemq/mqtt-cli sub \
  --topic 'stat/+/RESULT' \
  -h mqtt.home.hauke.cloud -p 1883

# Publish a command
podman run -it --rm docker.io/hivemq/mqtt-cli pub \
  --topic 'cmnd/<bridge_name>/ZbSend' \
  -m '{"device":"valve-kitchen","send":{"Power":1}}' \
  -h mqtt.home.hauke.cloud -p 1883
```

## Message Handling

- Subscribe with QoS 1 for sensor data (`tele/#`). QoS 0 is acceptable for command ACKs.
- Parse `tele/<bridge>/SENSOR` payloads: always check for `ZbReceived` key before processing.
- Ignore unknown device names gracefully (log at DEBUG level, never error).
- Implement a per-device channel (unbuffered or bounded to 10) to serialize state updates — never process the same device concurrently.
- Reconnect logic: use exponential backoff (1s → 2s → 4s … cap at 60s). Log each attempt at WARN.

## Sending Commands

Commands to Zigbee devices go through the Tasmota bridge via `cmnd/<bridge>/ZbSend`:

```json
{
  "device": "valve-kitchen",
  "send": { "Power": 1 }
}
```

- Always await a `stat/<bridge>/RESULT` ACK with a configurable timeout (default 5s).
- Expose command-sending as a `DeviceCommander` interface so REST handlers and the K8s reconciler share the same code path.

## Zigbee Device Capabilities

Model device capabilities as Go constants/iota, not raw strings:

```go
type Capability uint8
const (
    CapabilityOnOff Capability = iota
    CapabilityValve
    CapabilityTemperature
    CapabilityHumidity
    CapabilityBattery
)
```

Map Tasmota JSON keys to capabilities in a per-device-model registry (`internal/device/registry.go`).
