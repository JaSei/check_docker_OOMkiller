package main

import (
	"bytes"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/nlopes/slack"
	"github.com/olorin/nagiosplugin"
	"golang.org/x/net/context"
	"gopkg.in/alecthomas/kingpin.v2"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"text/template"
)

const VERSION = "3.0.0"

var (
	debug             = kingpin.Flag("debug", "Enable debug mode. Debug prints are print to STDERR").Bool()
	debugFile         = kingpin.Flag("debugFile", "Redirect debug prints to file (must be set debug option too)").String()
	format            = kingpin.Flag("format", "Format of output use go-templates like docker inspect").Default("Container {{.ID}} ({{.Config.Image}}) was killed by OOM killer").String()
	lastContainerFile = kingpin.Flag("store", "Path to file where is store last processed container").String()
	level             = kingpin.Flag("level", "Report OOMKilled containers warning or critical").Default("warning").Enum("warning", "critical")
	slackToken        = kingpin.Flag("slack", "Slack token, for reports problematic container to slack").String()
	slackChannels     = kingpin.Flag("slackChannel", "Slack channel for reports problematic container to slack").Strings()
	slackUser         = kingpin.Flag("slackUser", "Name which will be used as bot name").Default("OOM killer").String()
)

func main() {
	kingpin.Version(VERSION)
	kingpin.Parse()

	if *debug && *debugFile != "" {
		file, err := os.OpenFile(*debugFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0755)
		defer file.Close()

		if err != nil {
			logAndExit(fmt.Sprintf("Open file %s failed: %s", *debugFile, err.Error()))
		}

		log.SetOutput(file)
	}

	cli := createDockerClient()
	listOptions := prepareListOptions()
	addSinceFromFile(cli, &listOptions)

	containers, err := cli.ContainerList(context.Background(), listOptions)
	if err != nil {
		logAndExit(fmt.Sprintf("List docker containers: %s", err.Error()))
	}

	check := nagiosplugin.NewCheck()
	defer check.Finish()

	tmpl, err := template.New("format").Parse(*format)
	if err != nil {
		logAndExit(fmt.Sprintf("Prepare template failed: %s", err.Error()))
	}

	var lastContainer types.Container
	for i := range containers {
		//becuase order is from newer to older
		c := containers[len(containers)-1-i]

		if *debug {
			log.Printf("Inspecting container %s", c.ID)
		}

		containerInfo, err := cli.ContainerInspect(context.Background(), c.ID)
		if err != nil {
			logAndExit(fmt.Sprintf("Container %s inspect error: %s", c.ID, err.Error()))
		}

		if containerInfo.State.OOMKilled {
			if *debug {
				log.Printf("Found OOM killed container %s", c.ID)
			}

			message := new(bytes.Buffer)
			err := tmpl.Execute(message, containerInfo)
			if err != nil {
				logAndExit(fmt.Sprintf("Execute template failed: %s", err.Error()))
			}

			nagiosStatus := nagiosplugin.WARNING
			if *level == "critical" {
				nagiosStatus = nagiosplugin.CRITICAL
			}

			check.AddResult(nagiosStatus, message.String())

			if *slackToken != "" && len(*slackChannels) > 0 {
				var channelsOverrides []string

				slackContact, ok := c.Labels["SLACK_CONTACT"]
				if ok {
					strings.Replace(slackContact, " ", "", -1)
					strings.Replace(slackContact, "#", "", -1)
					slackContacts := strings.Split(slackContact, ",")

					channelsOverrides = make([]string, len(slackContacts))
					copy(channelsOverrides, slackContacts)
				} else {
					channelsOverrides = make([]string, len(*slackChannels))
					copy(channelsOverrides, *slackChannels)
				}

				for _, slackChannel := range channelsOverrides {
					err := reportToSlack(slackChannel, message.String())
					if err != nil {
						log.Printf("Send message to slack failed: %s", err)
					}
				}
			}
		}

		lastContainer = c
	}

	writeSinceToFile(*lastContainerFile, lastContainer.ID)

	check.AddResult(nagiosplugin.OK, "No OOM killed container")
}

func createDockerClient() *client.Client {
	defaultHeaders := map[string]string{"User-Agent": "engine-api-cli-1.0"}
	cli, err := client.NewClient("unix:///var/run/docker.sock", "v1.18", nil, defaultHeaders)
	if err != nil {
		logAndExit(fmt.Sprintf("Connect to docker: %s", err.Error()))
	}

	return cli
}

func prepareListOptions() types.ContainerListOptions {
	filterOptions := filters.NewArgs()
	filterOptions.Add("status", "exited")
	filterOptions.Add("status", "dead")

	return types.ContainerListOptions{All: true, Filters: filterOptions}
}

func addSinceFromFile(cli *client.Client, listOptions *types.ContainerListOptions) {
	if *lastContainerFile != "" {
		if _, err := os.Stat(*lastContainerFile); err == nil {
			if *debug {
				log.Printf("Load from file %s", *lastContainerFile)
			}

			sinceContainerIdByte, err := ioutil.ReadFile(*lastContainerFile)
			if err != nil {
				logAndExit(fmt.Sprintf("Read file %s failed: %s", *lastContainerFile, err.Error()))
			}

			sinceContainerId := strings.TrimSpace((string)(sinceContainerIdByte))

			if len(sinceContainerId) == 64 {
				if *debug {
					log.Printf("Loaded container %s from file", sinceContainerId)
				}

				_, err := cli.ContainerInspect(context.Background(), sinceContainerId)

				if err == nil {
					//docker 1.10
					listOptions.Since = sinceContainerId
					//docker 1.12
					listOptions.Filters.Add("since", sinceContainerId)
				} else if *debug {
					log.Printf("Loaded container %s don't exists", sinceContainerId)
				}
			}
		} else {
			if *debug {
				log.Printf("File %s doesn't load: %s", *lastContainerFile, err.Error())
			}
		}
	}
}

func writeSinceToFile(lastContainerFile, id string) {
	if lastContainerFile != "" && id != "" {
		err := ioutil.WriteFile(lastContainerFile, []byte(id), 0644)

		if err != nil {
			logAndExit(fmt.Sprintf("Write to file %s failed: %s", lastContainerFile, err.Error()))
		}
	}
}

func reportToSlack(slackChannel, report string) error {
	api := slack.New(*slackToken)
	params := slack.PostMessageParameters{
		Username:  *slackUser,
		LinkNames: 1,
	}
	_, _, err := api.PostMessage(slackChannel, report, params)

	return err
}

func logAndExit(exitMsg string) {
	if *debug {
		log.Println(exitMsg)
	}
	nagiosplugin.Exit(nagiosplugin.UNKNOWN, exitMsg)
}
