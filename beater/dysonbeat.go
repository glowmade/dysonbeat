package beater

import (
	"fmt"
	"net"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/glowmade/dysonproto/dyson"
	flatbuffers "github.com/glowmade/flatbuffers/go"

	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/publisher"

	"github.com/glowmade/dysonbeat/config"
)

// set by main(), encoded as tag to match up with logged ID in Slack
var CorrelationID string

type Glowbeat struct {
	done   chan struct{}
	config config.Config
	client publisher.Client
	udpcon *net.UDPConn
}

func New(b *beat.Beat, cfg *common.Config) (beat.Beater, error) {
	config := config.DefaultConfig
	if err := cfg.Unpack(&config); err != nil {
		return nil, fmt.Errorf("Error reading config file: %v", err)
	}

	bt := &Glowbeat{
		done:   make(chan struct{}),
		config: config,
	}
	return bt, nil
}

const (
	cUDPReadBufferSize = 1024 * 3
	cRecvReadBuffer    = cUDPReadBufferSize * 1024
)

func (bt *Glowbeat) Run(b *beat.Beat) error {

	bt.client = b.Publisher.Connect()

	hostAddr := fmt.Sprintf("localhost:%d", bt.config.Port)
	log.Info("_", CorrelationID, "_ opening ", hostAddr)

	// start up UDP listener to receive log messages
	addr, err := net.ResolveUDPAddr("udp", hostAddr)
	if err != nil {
		log.Errorf("UDP resolve failed: %s", err.Error())
		return err
	}

	bt.udpcon, err = net.ListenUDP(addr.Network(), addr)
	if err != nil {
		log.Errorf("UDP listen failed: %s", err.Error())
		return err
	}
	defer bt.udpcon.Close()

	err = bt.udpcon.SetReadBuffer(cRecvReadBuffer)
	if err != nil {
		log.Errorf("UDP opts error: %s", err.Error())
		return err
	}

	buf := make([]byte, cUDPReadBufferSize)

	log.Info("_", CorrelationID, "_ is *running*")

	for {
		// watch for cancellation signal
		select {
		case <-bt.done:
			return nil
		default:
		}

		// HDD think about goroutines to avoid read loss
		count, _, err := bt.udpcon.ReadFrom(buf)
		if err != nil {
			e, ok := err.(net.Error)
			if ok && e.Timeout() {
				continue
			}

			log.Errorf("UDP recv error: %s", err.Error())
			return err
		}

		if count < 8 {
			log.Errorf("UDP flatbuffer buffer too small : %d", count)
			continue
		}

		gotIdentifier, err := flatbuffers.GetIdentifier(buf)
		if err != nil {
			log.Errorf("UDP read %d, flatbuffer ident error: %s", count, err.Error())
			continue
		}

		if dyson.FlatLogTypeFourCC != gotIdentifier {
			log.Errorf("flatbeat - incorrect type id %s, expected %s", gotIdentifier, dyson.FlatLogTypeFourCC)
			continue
		}

		var fLog dyson.FlatLog
		fLog.Init(buf, flatbuffers.GetUOffsetT(buf))

		timeFromLog := common.Time(time.Unix(int64(fLog.Ts()), 0))

		event := common.MapStr{
			"type":       "log",
			"@timestamp": timeFromLog,
			"uid":        fLog.Uid(),
			"message":    string(fLog.Message()),
			"context":    string(fLog.Context()),
			"tags":       CorrelationID,
			"level":      fLog.Level(),
		}

		numFields := fLog.FieldsLength()
		numFieldPairs := numFields / 2
		if numFieldPairs > 0 {
			fieldMap := make(map[string]string, numFieldPairs)
			for i := 0; i < numFields; i += 2 {
				fieldMap[string(fLog.Fields(i))] = string(fLog.Fields(i + 1))
			}

			event["fields"] = fieldMap
		}

		bt.client.PublishEvent(event)
	}
}

func (bt *Glowbeat) Stop() {
	log.Info("_", CorrelationID, "_ is *stopping* ...")
	bt.client.Close()
	close(bt.done)
	bt.udpcon.SetReadDeadline(time.Now()) // trigger instant timeout on any blocked ReadFrom(), allowing a graceful exit via 'return nil'
}
