package main

import (
	"encoding/json"
	log "github.com/sirupsen/logrus"
	"os"
)

type Config struct {
	MQTT struct {
		URL      string
		Username string
		Password string
		ClientID string
		Topic    string
	}
	ListenAddr  string `json:"listen_addr"`
	RecoverTime string `json:"recover_time"`
	Debug       bool
}

func LoadConfig(file string) Config {
	f, err := os.Open(file)
	defer f.Close()
	if err != nil {
		log.WithError(err).Fatal("Failed to open config file")
	}

	hostname, _ := os.Hostname()
	config := Config{
		ListenAddr:  ":15002",
		RecoverTime: "10s",
		Debug:       false,
	}
	config.MQTT.ClientID = hostname
	config.MQTT.Topic = "feye/{{ .SerialID }}/{{ .Channel }}"

	jsonParser := json.NewDecoder(f)
	jsonParser.Decode(&config)
	return config
}
