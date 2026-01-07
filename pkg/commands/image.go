package commands

import (
	"context"
	"strings"

	"github.com/christophe-duc/lazypodman/pkg/utils"
	"github.com/fatih/color"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

// Image represents a Podman image
type Image struct {
	Name          string
	Tag           string
	ID            string
	Summary       ImageSummary
	Runtime       ContainerRuntime
	OSCommand     *OSCommand
	Log           *logrus.Entry
	PodmanCommand LimitedPodmanCommand
}

// Remove removes the image
func (i *Image) Remove(force bool) error {
	ctx := context.Background()
	return i.Runtime.RemoveImage(ctx, i.ID, force)
}

func getHistoryResponseItemDisplayStrings(layer ImageHistoryEntry) []string {
	tag := ""
	if len(layer.Tags) > 0 {
		tag = layer.Tags[0]
	}

	id := strings.TrimPrefix(layer.ID, "sha256:")
	if len(id) > 10 {
		id = id[0:10]
	}
	idColor := color.FgWhite
	if id == "<missing>" {
		idColor = color.FgBlue
	}

	dockerFileCommandPrefix := "/bin/sh -c #(nop) "
	createdBy := layer.CreatedBy
	if strings.Contains(layer.CreatedBy, dockerFileCommandPrefix) {
		createdBy = strings.Trim(strings.TrimPrefix(layer.CreatedBy, dockerFileCommandPrefix), " ")
		split := strings.Split(createdBy, " ")
		createdBy = utils.ColoredString(split[0], color.FgYellow) + " " + strings.Join(split[1:], " ")
	}

	createdBy = strings.Replace(createdBy, "\t", " ", -1)

	size := utils.FormatBinaryBytes(int(layer.Size))
	sizeColor := color.FgWhite
	if size == "0B" {
		sizeColor = color.FgBlue
	}

	return []string{
		utils.ColoredString(id, idColor),
		utils.ColoredString(tag, color.FgGreen),
		utils.ColoredString(size, sizeColor),
		createdBy,
	}
}

// RenderHistory renders the history of the image
func (i *Image) RenderHistory() (string, error) {
	ctx := context.Background()
	history, err := i.Runtime.ImageHistory(ctx, i.ID)
	if err != nil {
		return "", err
	}

	tableBody := lo.Map(history, func(layer ImageHistoryEntry, _ int) []string {
		return getHistoryResponseItemDisplayStrings(layer)
	})

	headers := [][]string{{"ID", "TAG", "SIZE", "COMMAND"}}
	table := append(headers, tableBody...)

	return utils.RenderTable(table)
}
