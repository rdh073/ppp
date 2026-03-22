package devicectrl

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/gorilla/websocket"
)

var adbWSUpgrader = websocket.Upgrader{
	// Allow connections from the dashboard (any origin in dev).
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// adbWS upgrades the HTTP connection to WebSocket and proxies ADB daemon
// protocol packets between the browser and the device's adbd via the ADB
// host server (localhost:5037).
//
// Framing contract: each binary WebSocket message = one complete ADB packet
// (24-byte little-endian header + payload). The TCP side is a raw byte stream;
// we re-frame it packet-by-packet.
func (h *DeviceHandler) adbWS(w http.ResponseWriter, r *http.Request, id domain.DeviceID) {
	log := h.log
	serial := h.resolveSerial(r, id)
	log.Info("adb-ws: connecting", "deviceId", id, "serial", serial)

	tcpConn, err := dialADBTransport(serial)
	if err != nil {
		log.Warn("adb-ws: dial failed", "serial", serial, "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer tcpConn.Close()

	wsConn, err := adbWSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Warn("adb-ws: ws upgrade failed", "err", err)
		return
	}
	defer wsConn.Close()

	log.Info("adb-ws: tunnel open", "deviceId", id, "serial", serial)
	errc := make(chan error, 2)

	// TCP → WebSocket: read ADB packets from the raw TCP stream and forward
	// each one as a single binary WebSocket message.
	go func() {
		header := make([]byte, 24)
		for {
			if _, err := io.ReadFull(tcpConn, header); err != nil {
				log.Info("adb-ws: tcp read error", "err", err)
				errc <- err
				return
			}
			payloadLen := binary.LittleEndian.Uint32(header[12:16])
			cmd := binary.LittleEndian.Uint32(header[0:4])
			log.Info("adb-ws: tcp→ws packet", "cmd", fmt.Sprintf("%08x", cmd), "payloadLen", payloadLen)
			packet := make([]byte, 24+payloadLen)
			copy(packet, header)
			if payloadLen > 0 {
				if _, err := io.ReadFull(tcpConn, packet[24:]); err != nil {
					log.Info("adb-ws: tcp payload read error", "err", err)
					errc <- err
					return
				}
			}
			if err := wsConn.WriteMessage(websocket.BinaryMessage, packet); err != nil {
				log.Info("adb-ws: ws write error", "err", err)
				errc <- err
				return
			}
		}
	}()

	// WebSocket → TCP: each binary message is one complete ADB packet; write
	// its raw bytes directly to the TCP stream.
	go func() {
		for {
			msgType, data, err := wsConn.ReadMessage()
			if err != nil {
				log.Info("adb-ws: ws read error", "err", err)
				errc <- err
				return
			}
			if msgType != websocket.BinaryMessage {
				continue
			}
			if len(data) >= 4 {
				cmd := binary.LittleEndian.Uint32(data[0:4])
				log.Info("adb-ws: ws→tcp packet", "cmd", fmt.Sprintf("%08x", cmd), "len", len(data))
			}
			if _, err := tcpConn.Write(data); err != nil {
				log.Info("adb-ws: tcp write error", "err", err)
				errc <- err
				return
			}
		}
	}()

	err = <-errc
	log.Info("adb-ws: tunnel closed", "deviceId", id, "reason", err)
}

// resolveSerial returns the ADB serial for the given device.
// Priority: binding store → ?serial query param → device ID string.
func (h *DeviceHandler) resolveSerial(r *http.Request, id domain.DeviceID) string {
	if h.bindings != nil {
		if binding, exists, err := h.bindings.GetBinding(r.Context(), id); err == nil && exists && binding.ADBSerial != "" {
			return binding.ADBSerial
		}
	}
	if s := r.URL.Query().Get("serial"); s != "" {
		return s
	}
	return string(id)
}

// dialADBTransport returns a raw TCP connection to the device's adbd that
// speaks the ADB daemon binary protocol.
//
// WiFi ADB (serial = "ip:port"): connects directly to adbd — no host server
// needed, adbd speaks daemon protocol natively on TCP port 5555.
//
// USB ADB: not supported via this proxy. Use WebUSB mode or enable WiFi ADB
// on the device first ("adb tcpip 5555", then "adb connect <device-ip>:5555").
func dialADBTransport(serial string) (net.Conn, error) {
	if !strings.Contains(serial, ":") {
		return nil, fmt.Errorf(
			"USB ADB device %q is not supported by the WebSocket proxy — "+
				"use WebUSB mode, or enable WiFi ADB first: adb tcpip 5555, then adb connect <ip>:5555",
			serial,
		)
	}
	// WiFi ADB: serial IS the TCP address of adbd (e.g. "192.168.1.10:5555").
	conn, err := net.DialTimeout("tcp", serial, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("cannot reach device adbd at %s: %w", serial, err)
	}
	return conn, nil
}
