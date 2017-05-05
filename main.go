package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/version"
	"github.com/multiplay/go-slack/chat"
	"github.com/multiplay/go-slack/lrhook"

	"github.com/glowmade/dysonbeat/beater"
)

var (
	// can be filled in by linker directive
	// Windows: go build -ldflags "-X main.builddate=%date%.%time%"
	//
	builddate string

	// webhook client ID T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX passed in as cmd arg
	slackWebhookID string

	// unique host name for this session, used as slack broadcast name
	hostID string
)

// https://stackoverflow.com/questions/23558425/how-do-i-get-the-local-ip-address-in-go
func getOutboundIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().String()
	idx := strings.LastIndex(localAddr, ":")

	return localAddr[0:idx], nil
}

func createUUID() (string, error) {
	uuid := make([]byte, 6)
	n, err := rand.Read(uuid)
	if n != len(uuid) || err != nil {
		return "", err
	}

	return hex.EncodeToString(uuid), nil
}

func createHostID() {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "host-error"
		fmt.Println(err)
	}

	extip, err := getOutboundIP()
	if err != nil {
		extip = "x.x.x.x"
		fmt.Println(err)
	}

	// stash the correlation uid to send with every event
	beater.CorrelationID, err = createUUID()
	if err != nil {
		beater.CorrelationID = "cid-error"
		fmt.Println(err)
	}

	hostID = fmt.Sprintf("[dysonbeat] %s {%s} : %s", hostname, extip, beater.CorrelationID)
}

func init() {

	createHostID()

	flag.StringVar(&slackWebhookID, "webhook", "", "pass in slack webhook ID for logging connection")
	flag.Parse()

	if len(slackWebhookID) > 0 {

		connectionString := fmt.Sprintf("https://hooks.slack.com/services/%s", slackWebhookID)

		cfg := lrhook.Config{
			Message: chat.Message{
				Username: hostID,
				Markdown: true,
				Parse:    "full",
			},
			Attachment: chat.Attachment{
				MarkdownIn: []string{"text"},
			},
			MinLevel: log.InfoLevel,
			// Async:    true,
		}
		h := lrhook.New(cfg, connectionString)
		log.AddHook(h)

	} else {

		fmt.Printf("No webhook ID, not connecting to Slack\n")
	}

	log.SetFormatter(&log.JSONFormatter{})
	log.SetOutput(os.Stderr)
	log.SetLevel(log.InfoLevel)
}

func stackTrace() {
	for i := 1; i < 10; i++ {
		pc, file, line, ok := runtime.Caller(i)
		if ok {
			fnc := runtime.FuncForPC(pc)
			fmt.Printf("[%4d] %-50s | %s\n", line, file, fnc.Name())
		}
	}
}

func main() {

	log.Infof("*Booting*\n`[dysonbeat %s]`\n`[libbeat v.%s]`", builddate, version.GetDefaultVersion())

	err := beat.Run("dysonbeat", "0.1.0", beater.New)
	if err != nil {
		log.Fatalf("ELK beat Run() error: %v", err)
	}

}
