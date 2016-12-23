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
	"strings"
	"text/template"
)

type config struct {
	debug             bool
	format            string
	lastContainerFile string
	level             nagiosplugin.Status
}

func main() {
	cfg := parseFlags()

	cli := createDockerClient()
	listOptions := prepareListOptions()
	addSinceFromFile(cli, &listOptions, cfg)

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
			check.AddResult(cfg.level, buf.String())
		}

		lastContainer = c
	}

	writeSinceToFile(cfg.lastContainerFile, lastContainer.ID)

	check.AddResult(nagiosplugin.OK, "No OOM killed container")
}

func parseFlags() config {
	debug := flag.Bool("debug", false, "Print debug prints to STDERR")
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

	return config{lastContainerFile: *lastContainerFile, level: level, format: *format, debug: *debug}
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

func addSinceFromFile(cli *client.Client, listOptions *types.ContainerListOptions, cfg config) {
	if cfg.lastContainerFile != "" {
		if _, err := os.Stat(cfg.lastContainerFile); err == nil {
			if cfg.debug {
				fmt.Fprintf(os.Stderr, "Load from file %s\n", cfg.lastContainerFile)
			}

			sinceContainerIdByte, err := ioutil.ReadFile(cfg.lastContainerFile)
			if err != nil {
				nagiosplugin.Exit(nagiosplugin.UNKNOWN, fmt.Sprintf("Read file %s failed: %s", cfg.lastContainerFile, err.Error()))
			}

			sinceContainerId := strings.TrimSpace((string)(sinceContainerIdByte))

			if len(sinceContainerId) == 64 {
				if cfg.debug {
					fmt.Fprintf(os.Stderr, "Loaded container %s from file\n", sinceContainerId)
				}

				_, err := cli.ContainerInspect(context.Background(), sinceContainerId)

				if err == nil {
					//docker 1.10
					listOptions.Since = sinceContainerId
					//docker 1.12
					listOptions.Filters.Add("since", sinceContainerId)
				} else if cfg.debug {
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
