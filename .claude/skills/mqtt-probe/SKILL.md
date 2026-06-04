---
name: mqtt-probe
description: Diagnose MQTT connectivity and Tasmota/Zigbee message flow. Use when a device is not reporting, commands are not reaching a device, or the MQTT bridge topic structure is unknown. Uses the HiveMQ MQTT CLI via podman to probe the broker and validates captured payloads against the Go device model.
---

# mqtt-probe

This skill is read-only and diagnostic — it does NOT send device commands unless you explicitly confirm in Step 4.

MQTT CLI command: `podman run -it --rm docker.io/hivemq/mqtt-cli`
Broker: `mqtt.home.hauke.cloud:1883` (no auth)

---

## Step 1 — Check TCP reachability

```bash
nc -z -w 3 mqtt.home.hauke.cloud 1883 && echo "TCP OK" || echo "TCP FAILED"
```

If this fails, stop — the problem is network-level. Check DNS and firewall before anything else.

---

## Step 2 — Subscribe to bridge traffic

Run two subscriptions in parallel to capture both telemetry and command results:

```bash
# Sensor data from all Tasmota bridges (30-second window)
podman run -it --rm docker.io/hivemq/mqtt-cli sub \
  --topic 'tele/+/SENSOR' \
  -h mqtt.home.hauke.cloud -p 1883 \
  --duration 30000

# Command ACKs from all bridges (run concurrently in a second terminal)
podman run -it --rm docker.io/hivemq/mqtt-cli sub \
  --topic 'stat/+/RESULT' \
  -h mqtt.home.hauke.cloud -p 1883 \
  --duration 30000
```

From the output, identify:
- The bridge name (the path segment between `tele/` and `/SENSOR`).
- `ZbReceived` blocks and the `Name` field inside them — this is the Tasmota friendly name.
- All JSON keys present for the target device.

If no `tele/+/SENSOR` messages appear in 30 s, the bridge is offline. If the bridge publishes but the target device is absent, the Zigbee device is offline or has not yet reported.

---

## Step 3 — Validate payload against the Go device model

Parse the captured `tele/<bridge>/SENSOR` payload and cross-check against `internal/device/registry.go`:

1. Extract all keys inside the `ZbReceived.<address>` block.
2. Look up the device's `TasmotaKeyMap` in the Go source.
3. Report:

```
Device: valve-kitchen (0x1234)  bridge: tasmota-bridge
  Payload keys:   StateValve, BatteryPercentage, LinkQuality, Endpoint
  Mapped keys:    StateValve ✓  BatteryPercentage ✓  LinkQuality ✓
  Unmapped keys:  Endpoint  ← add to TasmotaKeyMap if relevant
  Missing keys:   (none)
```

Unmapped keys mean data is arriving but being silently dropped. Flag these as a bug if they carry sensor values.

---

## Step 4 — Send a test command (CONFIRM BEFORE RUNNING)

**Stop and ask the user before this step** — it will actuate a physical device.

```bash
BRIDGE="<bridge_name>"       # discovered in Step 2
DEVICE="<friendly-name>"     # discovered in Step 2

# Send command
podman run -it --rm docker.io/hivemq/mqtt-cli pub \
  --topic "cmnd/${BRIDGE}/ZbSend" \
  -m "{\"device\":\"${DEVICE}\",\"send\":{\"Power\":1}}" \
  -h mqtt.home.hauke.cloud -p 1883

# Listen for the ACK (run immediately after)
podman run -it --rm docker.io/hivemq/mqtt-cli sub \
  --topic "stat/${BRIDGE}/RESULT" \
  -h mqtt.home.hauke.cloud -p 1883 \
  --duration 5000
```

Expected ACK: a `stat/<bridge>/RESULT` message containing `"ZbSent"` or an echo of the command. No message within 5 s means the Zigbee device is unreachable at the radio layer.

---

## Step 5 — Report findings

```
MQTT Probe Report — <timestamp>
Broker:        mqtt.home.hauke.cloud:1883
Bridge:        <bridge_name>

TCP:           OK | FAILED
Bridge alive:  YES | NO (no tele/+/SENSOR in 30s)

Devices seen:
  <address> (<name>)  LQ=<lq>  battery=<bat>%

Payload mapping:
  <device>: all keys mapped | N unmapped keys: <list>

Command ACK:   OK | TIMEOUT | NOT TESTED
```

---

## Verification checklist

- [ ] TCP reachability confirmed
- [ ] `tele/+/SENSOR` subscription ran for at least 30 seconds
- [ ] Bridge name identified from live topic
- [ ] Payload keys cross-checked against `TasmotaKeyMap` in Go source
- [ ] Test command only sent after explicit user confirmation
- [ ] Report produced with all sections filled
