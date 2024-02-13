package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/spangenberg/somweb/internal"
)

var duration = 5 * time.Second

type SomwebConfig struct {
	Host     string
	Username string
	Password string
}

type MqttConfig struct {
	Broker string
	Port   string
	User   string
	Pass   string
}

type Config struct {
	Somweb SomwebConfig
	Mqtt   MqttConfig
}

func connectHandler(client mqtt.Client) {
	log.Println("Connected")
}

func connectLostHandler(client mqtt.Client, err error) {
	log.Printf("Connect lost: %v", err)
}

func messagePubHandler(client mqtt.Client, msg mqtt.Message) {
	log.Printf("Received message: %s from topic: %s\n", msg.Payload(), msg.Topic())
}

func boolToString(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func tick(client *internal.Client, mqttClient mqtt.Client) error {
	doorStates, err := client.Doors()
	if err != nil {
		return fmt.Errorf("error getting door states: %v", err)
	}
	for i := 1; i <= 10; i++ {
		payload := boolToString(doorStates[fmt.Sprintf("%d", i)])
		if token := mqttClient.Publish(fmt.Sprintf("somweb/door/%d", i), 0, false, payload); token.Wait() && token.Error() != nil {
			return fmt.Errorf("error publishing door state: %v", token.Error())
		}
	}
	return nil
}

func newSomwebClient(cfg *Config) (*internal.Client, error) {
	client, err := internal.NewClient(cfg.Somweb.Host, cfg.Somweb.Username, cfg.Somweb.Password)
	if err != nil {
		return nil, fmt.Errorf("error creating somweb client: %v", err)
	}
	return client, nil
}

func newMqttClient(cfg *Config) (mqtt.Client, error) {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%s", cfg.Mqtt.Broker, cfg.Mqtt.Port))
	opts.SetClientID("somweb")
	opts.SetUsername(cfg.Mqtt.User)
	opts.SetPassword(cfg.Mqtt.Pass)
	opts.SetDefaultPublishHandler(messagePubHandler)
	opts.SetOnConnectHandler(connectHandler)
	opts.SetConnectionLostHandler(connectLostHandler)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return nil, fmt.Errorf("error connecting to mqtt broker: %v", token.Error())
	}
	return client, nil
}

func main() {
	cfg := &Config{
		Somweb: SomwebConfig{
			Host:     os.Getenv("SOMWEB_HOST"),
			Username: os.Getenv("SOMWEB_USERNAME"),
			Password: os.Getenv("SOMWEB_PASSWORD"),
		},
		Mqtt: MqttConfig{
			Broker: os.Getenv("MQTT_BROKER"),
			Port:   os.Getenv("MQTT_PORT"),
			User:   os.Getenv("MQTT_USER"),
			Pass:   os.Getenv("MQTT_PASS"),
		},
	}

	var err error
	var somwebClient *internal.Client
	if somwebClient, err = newSomwebClient(cfg); err != nil {
		panic(fmt.Errorf("error creating somweb client: %v", err))
	}
	var mqttClient mqtt.Client
	if mqttClient, err = newMqttClient(cfg); err != nil {
		panic(fmt.Errorf("error creating mqtt client: %v", err))
	}

	mqttClient.Subscribe("somweb/close/#", 0, func(client mqtt.Client, msg mqtt.Message) {
		door := strings.TrimPrefix(msg.Topic(), "somweb/close/")
		payload := string(msg.Payload())
		if payload == "1" {
			if err = somwebClient.CloseDoor(door); err != nil {
				panic(err)
			}
		}
	})
	mqttClient.Subscribe("somweb/open/#", 0, func(client mqtt.Client, msg mqtt.Message) {
		door := strings.TrimPrefix(msg.Topic(), "somweb/open/")
		payload := string(msg.Payload())
		if payload == "1" {
			if err = somwebClient.OpenDoor(door); err != nil {
				panic(err)
			}
		}
	})
	mqttClient.Subscribe("somweb/toggle/#", 0, func(client mqtt.Client, msg mqtt.Message) {
		door := strings.TrimPrefix(msg.Topic(), "somweb/toggle/")
		payload := string(msg.Payload())
		if payload == "1" {
			if err = somwebClient.ToggleDoor(door); err != nil {
				panic(err)
			}
		}
	})

	d := time.NewTicker(duration)
	defer d.Stop()

	m := &sync.Mutex{}
	go loop(d, m, somwebClient, mqttClient)

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	<-done
	d.Stop()
	m.Lock()
	mqttClient.Disconnect(250)
	m.Unlock()
}

func loop(d *time.Ticker, m *sync.Mutex, somwebClient *internal.Client, mqttClient mqtt.Client) {
	func() {
		for {
			select {
			case <-d.C:
				m.Lock()
				if err := tick(somwebClient, mqttClient); err != nil {
					log.Println(fmt.Errorf("error ticking: %v", err))
				}
				m.Unlock()
			}
		}
	}()
}
