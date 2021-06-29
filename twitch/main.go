package main

import (
	_ "embed"
	"encoding/json"
	"os"
	"time"

	"github.com/moniquelive/moniquelive-bot/twitch/commands"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
)

//go:embed .oauth
var oauth string

var cmd commands.Commands

const (
	username           = "moniquelive_bot"
	queueName          = "ms.twitch"
	createTtsTopicName = "create_tts"
	spotifyTopicName   = "spotify_music_updated"
)

var (
	amqpURL  = os.Getenv("RABBITMQ_URL")
	redisURL = os.Getenv("REDIS_URL")
	log      = logrus.WithField("package", "main")
)

func init() {
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: time.StampMilli,
	})
	logrus.SetLevel(logrus.TraceLevel) // sets log level
	cmd.Reload()
}

func check(err error) {
	if err != nil {
		log.Fatalln("failed:", err)
	}
}

func main() {
	defer log.Debugln("AMQP consumer shutdown.")

	go NewWatcher()

	var err error

	conn, err := amqp.Dial(amqpURL)
	check(err)
	defer conn.Close()
	go func() { log.Debugf("closing: %s", <-conn.NotifyClose(make(chan *amqp.Error))) }()

	log.Debugln("got Connection, getting Channel")
	channel, err := conn.Channel()
	check(err)
	defer channel.Close()

	log.Debugf("declaring Queue %q", queueName)
	queue, err := channel.QueueDeclare(
		queueName, // name of the queue
		true,      // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // noWait
		nil,       // arguments
	)
	check(err)

	log.Debugf("binding Queue %q to amq.topic", queueName)
	err = channel.QueueBind(queueName, spotifyTopicName, "amq.topic", false, nil)
	check(err)

	log.Debugln("Setting QoS")
	err = channel.Qos(1, 0, true)
	check(err)

	log.Debugf("declared Queue (%q %d messages, %d consumers)", queue.Name, queue.Messages, queue.Consumers)

	tag := uuid.NewString()
	log.Debugf("starting Consume (tag:%q)", tag)
	deliveries, err := channel.Consume(
		queue.Name, // name
		tag,        // consumerTag,
		false,      // noAck
		false,      // exclusive
		false,      // noLocal
		false,      // noWait
		nil,        // arguments
	)
	check(err)

	client, err := NewTwitch(username, oauth, &cmd, channel)
	if err != nil {
		log.Panicln("NewTwitch(): ", err)
	}

	go handle(deliveries, client)

	err = client.Connect()
	if err != nil {
		log.Panicln("client.Connect(): ", err)
	}

	//// wait for interrupt signal
	//stopChan := make(chan os.Signal, 1)
	//signal.Notify(stopChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	//for {
	//	select {
	//	case <-stopChan:
	//		// will close the deliveries channel
	//		err = channel.Cancel(tag, true)
	//		check(err)
	//
	//	// wait for go handle(...)
	//	case <-deliveriesDoneChan:
	//		return
	//	}
	//}
}

func handle(deliveries <-chan amqp.Delivery, client *Twitch) {
	defer log.Println("AMQP Handler: Exiting from deliveries handler")

	log.Debugln("Listening...")
	for delivery := range deliveries {
		if delivery.Body == nil {
			return
		}
		_ = delivery.Ack(false)

		message := string(delivery.Body)
		if message == "" {
			log.Debugln("empty message. ignoring...")
			continue
		}
		log.Infoln("DELIVERY:", message)

		var si struct {
			ImgUrl string `json:"imgUrl"`
			Title  string `json:"title"`
			Artist string `json:"artist"`
		}
		err := json.Unmarshal(delivery.Body, &si)
		if err != nil {
			log.Errorln("handle > json.Unmarshal:", err)
			continue
		}
		client.Say("/color Chocolate")
		client.Say("/me " + si.Artist + " - " + si.Title + " - " + si.ImgUrl)
	}
}
