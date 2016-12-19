package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/olorin/nagiosplugin"
	"golang.org/x/net/context"
	"io/ioutil"
	"os"
	"text/template"
)

type config struct {
	format            string
	lastContainerFile string
	level             nagiosplugin.Status
}

func main() {
	cfg := parseFlags()

	cli := createDockerClient()
	listOptions := prepareListOptions()
	addSinceFromFile(&listOptions, cfg.lastContainerFile)
	fmt.Println(listOptions)

	containers, err := cli.ContainerList(context.Background(), listOptions)
	if err != nil {
		nagiosplugin.Exit(nagiosplugin.UNKNOWN, fmt.Sprintf("List docker containers: %s", err.Error()))
	}

	check := nagiosplugin.NewCheck()
	defer check.Finish()

	tmpl, err := template.New("format").Parse(cfg.format)
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

			buf.String()
			check.AddResult(cfg.level, fmt.Sprintln(buf.String()))
		}

		lastContainer = c
	}

	writeSinceToFile(cfg.lastContainerFile, lastContainer.ID)

	check.AddResult(nagiosplugin.OK, "No OOM killed container")
}

func parseFlags() config {
	format := flag.String("format", "Container {{.ID}} ({{.Config.Image}}) was killed by OOM killer", "Format of output use go-templates like docker inspect")
	lastContainerFile := flag.String("l", "", "Path to file where is store last processed container")
	warning := flag.Bool("w", false, "Report OOMKilled container as warning")
	critical := flag.Bool("c", false, "Report OOMKilled container as critical")

	flag.Parse()

	level := nagiosplugin.WARNING
	if *warning && *critical {
		nagiosplugin.Exit(nagiosplugin.UNKNOWN, fmt.Sprintf("Can be set only warning or critical option"))
	} else if *critical {
		level = nagiosplugin.CRITICAL
	}

	return config{lastContainerFile: *lastContainerFile, level: level, format: *format}
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

func addSinceFromFile(listOptions *types.ContainerListOptions, lastContainerFile string) {
	if lastContainerFile != "" {
		if _, err := os.Stat(lastContainerFile); err == nil {
			sinceContainerIdByte, err := ioutil.ReadFile(lastContainerFile)
			if err != nil {
				nagiosplugin.Exit(nagiosplugin.UNKNOWN, fmt.Sprintf("Read file %s failed: %s", lastContainerFile, err.Error()))
			}

			if len((string)(sinceContainerIdByte)) == 64 {
				listOptions.Since = (string)(sinceContainerIdByte)
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
