package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	log "github.com/sirupsen/logrus"
	"io"
	"net"
	"sync"
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
	mqtt          MQTT.Client
	timerMap      sync.Map
	cancelMap     sync.Map
	timerDuration time.Duration
	configFile string
)
func init() {
	flag.StringVar(&configFile, "config", "", "Config file path")
	flag.Parse()
}


func setupMQTT(config Config) error {
	opts := MQTT.NewClientOptions()
	opts.AddBroker(config.MQTT.URL)
	opts.SetClientID(config.MQTT.ClientID)
	opts.SetCleanSession(true)

	if config.MQTT.Username != "" {
		opts.SetUsername(config.MQTT.Username)
		if config.MQTT.Password != "" {
			opts.SetPassword(config.MQTT.Password)
		}
	}

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

func publishState(topic string, packet Packet) {
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
		if c, ok := cancelMap.Load(top.String()); !ok {
			c = make(chan bool)
			cancelMap.Store(top.String(), c)
		}
		if t, ok := timerMap.Load(top.String()); ok {
			c, _ := cancelMap.Load(top.String())
			c.(chan bool) <- true
			timer := t.(*time.Timer)
			timer.Stop()
		}

		timer := time.NewTimer(timerDuration)
		timerMap.Store(top.String(), timer)
		go func(timer *time.Timer, mqtt MQTT.Client, topic string, status string) {
			cancelChan, _ := cancelMap.Load(topic)
			select {
			case <-timer.C:
			case <-cancelChan.(chan bool):
				log.Debug("Recieved message to cancel waiting to send stop message")
				return
			}
			token = mqtt.Publish(topic, 0, true, status)
			token.Wait()
			if token.Error() != nil {
				log.Error(token.Error())
			} else {
				log.WithField("topic", topic).WithField("status", status).Debug("Published message to MQTT")
			}
			timerMap.Delete(topic)
		}(timer, mqtt, top.String(), packet.Status)
	}
}

func handleConnection(conn net.Conn, topic string) {
	defer conn.Close()
	log.Debug("New connection!")
	packet, err := ParsePacket(conn)
	if err != nil {
		log.Error(err)
		return
	}

	publishState(topic, packet)
}

func main() {
	config := LoadConfig(configFile)
	if config.Debug {
		log.SetLevel(log.DebugLevel)
	}

	ln, err := net.Listen("tcp", config.ListenAddr)
	if err != nil {
		log.WithError(err).Fatal("Failed to listen port")
		return
	}

	if err := setupMQTT(config); err != nil {
		log.Fatal(err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Error(err)
			continue
		}
		go handleConnection(conn, config.MQTT.Topic)
	}
}
