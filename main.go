package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/calehh/cmt-watcher/log"
	"github.com/ethereum/go-ethereum/ethclient"
	cli "gopkg.in/urfave/cli.v1"
	yaml "gopkg.in/yaml.v2"
)

var (
	OriginCommandHelpTemplate = `{{.Name}}{{if .Subcommands}} command{{end}}{{if .Flags}} [command options]{{end}} {{.ArgsUsage}}
{{if .Description}}{{.Description}}
{{end}}{{if .Subcommands}}
SUBCOMMANDS:
  {{range .Subcommands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
  {{end}}{{end}}{{if .Flags}}
OPTIONS:
{{range $.Flags}}   {{.}}
{{end}}
{{end}}`
)
var app *cli.App
var client *http.Client

func init() {
	client = &http.Client{Timeout: time.Minute * 3}
}

var (
	configPathFlag = cli.StringFlag{
		Name:  "config",
		Usage: "config path",
		Value: "./config.yml",
	}
	logLevelFlag = cli.IntFlag{
		Name:  "log",
		Usage: "log level",
		Value: log.InfoLog,
	}
	logFilePath = cli.StringFlag{
		Name:  "logPath",
		Usage: "log root path",
		Value: "/app/golang/proxylog",
	}
)

func init() {
	app = cli.NewApp()
	app.Version = "v1.0.0"
	app.Commands = []cli.Command{
		commandStart,
	}

	cli.CommandHelpTemplate = OriginCommandHelpTemplate
}

func main() {
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var commandStart = cli.Command{
	Name:  "start",
	Usage: "start loading contract gas fee",
	Flags: []cli.Flag{
		configPathFlag,
		logLevelFlag,
		logFilePath,
	},
	Action: Start,
}

type Config struct {
	GethUrl string `yaml:"geth_url"`
	WebHool string `yaml:"web_hook"`
}

var BlockNumber uint64

func Start(ctx *cli.Context) {
	logLevel := ctx.Int(logLevelFlag.Name)
	fmt.Println("log level", logLevel)
	logPath := ctx.String(logFilePath.Name)

	filename := fmt.Sprintf("/proxy_%v.log", strings.ReplaceAll(time.Now().Format("2006-01-02 15:04:05"), " ", "_"))
	fmt.Println("log file path", logPath+filename)
	logFile, err := os.Create(logPath + filename)
	if err != nil {
		panic(err)
	}
	defer logFile.Close()

	log.InitLog(log.DebugLog, logFile)

	configPath := ctx.String(configPathFlag.Name)
	configData, err := ioutil.ReadFile(configPath)
	if err != nil {
		panic(err)
	}

	var config Config
	yaml.Unmarshal(configData, &config)
	//update blockNumber
	go func() {
		for {
			time.Sleep(time.Minute)
			client, err := ethclient.Dial(config.GethUrl)
			if err != nil {
				continue
			}
			blockNumber, err := client.BlockNumber(context.Background())
			if err != nil {
				continue
			}
			log.Info("update block number", blockNumber)
			BlockNumber = blockNumber
		}
	}()

	go func() {
		ticker := time.NewTicker(time.Minute * 5)
		lastNumber := BlockNumber
		for {
			<-ticker.C
			if (BlockNumber - lastNumber) < 40 {
				msg := fmt.Sprintf("stop new blocks %v,%v", BlockNumber, lastNumber)
				Post(config.WebHool, msg)
				log.Warn("no new block")
			}
			log.Info("check pass", BlockNumber, lastNumber)
			lastNumber = BlockNumber
		}
	}()

	waitToExit()
}

// curl 'https://oapi.dingtalk.com/robot/send?access_token=d29bf8c8053c36df765a660eea01dcd0b441695486b0797a1afdb7278e198fbb' -H 'Content-Type: application/json' -d '{"msgtype": "text","text": {"content":"我就是我, 是不一样的烟火"}}'

type DingData struct {
	MsgType string      `json:"msgtype"`
	Text    DingContent `json:"text"`
}

type DingContent struct {
	Content string `json:"content"`
}

func Post(relayUrl string, msg string) (*http.Response, error) {
	data := DingData{
		MsgType: "text",
		Text:    DingContent{Content: msg},
	}
	dataEncoded, err := json.Marshal(&data)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	return client.Post(relayUrl, "application/json", strings.NewReader(string(dataEncoded)))
}

func waitToExit() {
	exit := make(chan bool, 0)
	sc := make(chan os.Signal, 1)
	if !signal.Ignored(syscall.SIGHUP) {
		signal.Notify(sc, syscall.SIGHUP)
	}
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for sig := range sc {
			fmt.Printf("received exit signal:%v", sig.String())
			close(exit)
			break
		}
	}()
	<-exit
}
