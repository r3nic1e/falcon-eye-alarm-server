package main

import (
	"bytes"
	"sync"
	"encoding/binary"
	"encoding/json"
	"io"
	log "github.com/sirupsen/logrus"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"net"
	"os"
	"flag"
	"text/template"
	"time"
)

type Packet struct {
	Header      [10]uint16
	Address     string
	Channel     uint8
	Description string `json:"Descrip"`
	Event       string
	SerialID    string
	StartTime   string
	Status      string
	Type        string
}

const (
	STATUS_START = "Start"
	STATUS_STOP  = "Stop"
)

var (
	mqtt MQTT.Client
	listenAddr string
	mqttServer string
	clientID string
	topic string
	timerMap sync.Map
	timerDuration time.Duration
)

func init() {
	hostname, _ := os.Hostname()
	flag.StringVar(&listenAddr, "listen", ":15002", "Listen address")
	flag.StringVar(&mqttServer, "server", "", "Full URL of the MQTT server")
	flag.StringVar(&clientID, "clientid", hostname, "ClientID for MQTT connection")
	flag.StringVar(&topic, "topic", "feye/{{ .SerialID }}/{{ .Channel }}", "Topic to publish to")
	flag.DurationVar(&timerDuration, "recover", 10 * time.Second, "Recover time for Stop event")
	debug := flag.Bool("debug", false, "")
	flag.Parse()

	if *debug {
		log.SetLevel(log.DebugLevel)
	}
}

func setupMQTT() error {
	opts := MQTT.NewClientOptions()
	opts.AddBroker(mqttServer)
	opts.SetClientID(clientID)
	opts.SetCleanSession(true)
	/*
	if username != "" {
		opts.SetUsername(username)
		if password != "" {
			opts.SetPassword(password)
		}
	}
	*/

	mqtt = MQTT.NewClient(opts)
	if token := mqtt.Connect(); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	log.Info("Connected to MQTT broker")
	return nil
}

func ParsePacket(r io.Reader) (Packet, error) {
	var packet Packet
	if err := binary.Read(r, binary.LittleEndian, &(packet.Header)); err != nil {
		return packet, err
	}

	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&packet); err != nil {
		return packet, err
	}

	return packet, nil
}

func publishState(packet Packet) {
	tmpl, err := template.New("topic").Parse(topic)
	if err != nil {
		log.Error(err)
		return
	}
	var top bytes.Buffer
	err = tmpl.Execute(&top, packet)
	if err != nil {
		log.Error(err)
		return
	}

	var token MQTT.Token
	switch packet.Status {
	case STATUS_START:
		token = mqtt.Publish(top.String(), 0, true, packet.Status)
		token.Wait()
		if token.Error() != nil {
			log.Error(token.Error())
		} else {
			log.WithField("topic", top.String()).WithField("status", packet.Status).Debug("Published message to MQTT")
		}
	case STATUS_STOP:
		if t, ok := timerMap.Load(top.String()); ok {
			timer := t.(*time.Timer)
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(timerDuration)
		} else {
			timer := time.NewTimer(timerDuration)
			timerMap.Store(top.String(), timer)
			go func(timer *time.Timer, mqtt MQTT.Client, topic string, status string) {
				<-timer.C
				token = mqtt.Publish(topic, 0, true, status)
				token.Wait()
				if token.Error() != nil {
					log.Error(token.Error())
				} else {
					log.WithField("topic", topic).WithField("status", status).Debug("Published message to MQTT")
				}
			}(timer, mqtt, top.String(), packet.Status)

		}
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	log.Debug("New connection!")
	packet, err := ParsePacket(conn)
	if err != nil {
		log.Error(err)
		return
	}

	publishState(packet)
}

func main() {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatal(err)
		return
	}

	if err := setupMQTT(); err != nil {
		log.Fatal(err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Error(err)
			continue
		}
		go handleConnection(conn)
	}
}
