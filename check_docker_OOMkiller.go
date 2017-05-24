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
	"os"
	"strings"
	"text/template"
)

const VERSION = "1.1.0"

var (
	debug             = kingpin.Flag("debug", "Print debug prints to STDERR").Bool()
	format            = kingpin.Flag("format", "Format of output use go-templates like docker inspect").Default("Container {{.ID}} ({{.Config.Image}}) was killed by OOM killer").String()
	lastContainerFile = kingpin.Flag("store", "Path to file where is store last processed container").String()
	level             = kingpin.Flag("level", "Report OOMKilled containers warning or critical").Default("warning").Enum("warning", "critical")
	slackToken        = kingpin.Flag("slack", "Slack token, for reports problematic container to slack").String()
	slackChannels     = kingpin.Flag("slackChannel", "Slack channel for reports problematic container to slack").Strings()
)

func main() {
	kingpin.Version(VERSION)
	kingpin.Parse()

	cli := createDockerClient()
	listOptions := prepareListOptions()
	addSinceFromFile(cli, &listOptions)

	containers, err := cli.ContainerList(context.Background(), listOptions)
	if err != nil {
		nagiosplugin.Exit(nagiosplugin.UNKNOWN, fmt.Sprintf("List docker containers: %s", err.Error()))
	}

	check := nagiosplugin.NewCheck()
	defer check.Finish()

	tmpl, err := template.New("format").Parse(*format)
	if err != nil {
		nagiosplugin.Exit(nagiosplugin.UNKNOWN, fmt.Sprintf("Prepare template failed: %s", err.Error()))
	}

	var lastContainer types.Container
	for i := range containers {
		//becuase order is from newer to older
		c := containers[len(containers)-1-i]

		containerInfo, err := cli.ContainerInspect(context.Background(), c.ID)
		if err != nil {
			nagiosplugin.Exit(nagiosplugin.UNKNOWN, fmt.Sprintf("Container %s inspect error: %s", c.ID, err.Error()))
		}

		if containerInfo.State.OOMKilled {
			buf := new(bytes.Buffer)
			err := tmpl.Execute(buf, containerInfo)
			if err != nil {
				nagiosplugin.Exit(nagiosplugin.UNKNOWN, fmt.Sprintf("Execute template failed: %s", err.Error()))
			}

			nagiosStatus := nagiosplugin.WARNING
			if *level == "critical" {
				nagiosStatus = nagiosplugin.CRITICAL
			}

			check.AddResult(nagiosStatus, buf.String())

			if *slackToken != "" && len(*slackChannels) > 0 {

				slackMessage := new(bytes.Buffer)

				slackContact, ok := c.Labels["SLACK_CONTACT"]
				if ok {
					slackMessage.WriteString(slackContact + ": ")
				}
				slackMessage.WriteString(buf.String())

				for _, slackChannel := range *slackChannels {
					err := reportToSlack(slackChannel, slackMessage.String())
					if err != nil {
						nagiosplugin.Exit(nagiosplugin.UNKNOWN, fmt.Sprintf("Send message to slack failed: %s", err))
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
		nagiosplugin.Exit(nagiosplugin.UNKNOWN, fmt.Sprintf("Connect to docker: %s", err.Error()))
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
				fmt.Fprintf(os.Stderr, "Load from file %s\n", *lastContainerFile)
			}

			sinceContainerIdByte, err := ioutil.ReadFile(*lastContainerFile)
			if err != nil {
				nagiosplugin.Exit(nagiosplugin.UNKNOWN, fmt.Sprintf("Read file %s failed: %s", *lastContainerFile, err.Error()))
			}

			sinceContainerId := strings.TrimSpace((string)(sinceContainerIdByte))

			if len(sinceContainerId) == 64 {
				if *debug {
					fmt.Fprintf(os.Stderr, "Loaded container %s from file\n", sinceContainerId)
				}

				_, err := cli.ContainerInspect(context.Background(), sinceContainerId)

				if err == nil {
					//docker 1.10
					listOptions.Since = sinceContainerId
					//docker 1.12
					listOptions.Filters.Add("since", sinceContainerId)
				} else if *debug {
					fmt.Fprintf(os.Stderr, "Loaded container %s don't exists\n", sinceContainerId)
				}
			}
		}
	}
}

func writeSinceToFile(lastContainerFile, id string) {
	if lastContainerFile != "" && id != "" {
		err := ioutil.WriteFile(lastContainerFile, []byte(id), 0644)

		if err != nil {
			nagiosplugin.Exit(nagiosplugin.UNKNOWN, fmt.Sprintf("Write to file %s failed: %s", lastContainerFile, err.Error()))
		}
	}
}

func reportToSlack(slackChannel, report string) error {
	api := slack.New(*slackToken)
	params := slack.PostMessageParameters{
		Username:  "mmcloud OOM killer",
		LinkNames: 1,
	}
	_, _, err := api.PostMessage(slackChannel, report, params)

	return err
}
