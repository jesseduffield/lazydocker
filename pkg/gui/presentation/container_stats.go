package presentation

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/christophe-duc/lazypodman/pkg/commands"
	"github.com/christophe-duc/lazypodman/pkg/config"
	"github.com/christophe-duc/lazypodman/pkg/utils"
	"github.com/fatih/color"
	"github.com/jesseduffield/asciigraph"
	"github.com/mcuadros/go-lookup"
	"github.com/samber/lo"
)

func RenderStats(userConfig *config.UserConfig, container *commands.Container, viewWidth int) (string, error) {
	stats, ok := container.GetLastStats()
	if !ok {
		return "", nil
	}

	graphSpecs := userConfig.Stats.Graphs
	graphs := make([]string, len(graphSpecs))
	for i, spec := range graphSpecs {
		graph, err := plotGraph(container, spec, viewWidth-10)
		if err != nil {
			return "", err
		}
		graphs[i] = utils.ColoredString(graph, utils.GetColorAttribute(spec.Color))
	}

	pidsCount := fmt.Sprintf("PIDs: %d", stats.ClientStats.PidsStats.Current)
	dataReceived := fmt.Sprintf("Traffic received: %s", utils.FormatDecimalBytes(stats.ClientStats.Networks.Eth0.RxBytes))
	dataSent := fmt.Sprintf("Traffic sent: %s", utils.FormatDecimalBytes(stats.ClientStats.Networks.Eth0.TxBytes))

	originalStats, err := utils.MarshalIntoYaml(stats)
	if err != nil {
		return "", err
	}

	contents := fmt.Sprintf("\n\n%s\n\n%s\n\n%s\n%s\n\n%s",
		utils.ColoredString(strings.Join(graphs, "\n\n"), color.FgGreen),
		pidsCount,
		dataReceived,
		dataSent,
		utils.ColoredYamlString(string(originalStats)),
	)

	return contents, nil
}

// plotGraph returns the plotted graph based on the graph spec and the stat history
func plotGraph(container *commands.Container, spec config.GraphConfig, width int) (string, error) {
	container.StatsMutex.Lock()
	defer container.StatsMutex.Unlock()

	data := make([]float64, len(container.StatHistory))

	for i, stats := range container.StatHistory {
		value, err := lookup.LookupString(stats, spec.StatPath)
		if err != nil {
			return "Could not find key: " + spec.StatPath, nil
		}
		floatValue, err := getFloat(value.Interface())
		if err != nil {
			return "", err
		}

		data[i] = floatValue
	}

	max := spec.Max
	if spec.MaxType == "" {
		max = lo.Max(data)
	}

	min := spec.Min
	if spec.MinType == "" {
		min = lo.Min(data)
	}

	height := 10
	if spec.Height > 0 {
		height = spec.Height
	}

	caption := fmt.Sprintf(
		"%s: %0.2f (%v)",
		spec.Caption,
		data[len(data)-1],
		time.Since(container.StatHistory[0].RecordedAt).Round(time.Second),
	)

	return asciigraph.Plot(
		data,
		asciigraph.Height(height),
		asciigraph.Width(width),
		asciigraph.Min(min),
		asciigraph.Max(max),
		asciigraph.Caption(caption),
	), nil
}

// RenderPodStats renders pod statistics as a string
func RenderPodStats(pod *commands.Pod, viewWidth int) (string, error) {
	stats, ok := pod.GetLastStats()
	if !ok {
		return "Collecting pod stats...", nil
	}

	s := stats.Stats

	// Format stats display
	var output strings.Builder

	output.WriteString(utils.ColoredString("\nPod Statistics\n\n", color.FgCyan))

	// CPU usage
	output.WriteString(fmt.Sprintf("CPU Usage:     %.2f%%\n", s.CPU))

	// Memory usage
	memUsed := utils.FormatBinaryBytes(int(s.MemUsage))
	memLimit := utils.FormatBinaryBytes(int(s.MemLimit))
	output.WriteString(fmt.Sprintf("Memory Usage:  %s / %s (%.2f%%)\n", memUsed, memLimit, s.Memory))

	// Network I/O
	netIn := utils.FormatDecimalBytes(int(s.NetInput))
	netOut := utils.FormatDecimalBytes(int(s.NetOutput))
	output.WriteString(fmt.Sprintf("Network I/O:   %s / %s\n", netIn, netOut))

	// Block I/O
	blockIn := utils.FormatDecimalBytes(int(s.BlockInput))
	blockOut := utils.FormatDecimalBytes(int(s.BlockOutput))
	output.WriteString(fmt.Sprintf("Block I/O:     %s / %s\n", blockIn, blockOut))

	// PIDs
	output.WriteString(fmt.Sprintf("PIDs:          %d\n", s.PIDs))

	// Render graphs if there's history
	pod.StatsMutex.Lock()
	historyLen := len(pod.StatHistory)
	pod.StatsMutex.Unlock()

	if historyLen > 1 {
		output.WriteString("\n")

		// CPU graph
		cpuGraph := plotPodGraph(pod, "Stats.CPU", "CPU %", viewWidth-10, 0, 100)
		output.WriteString(utils.ColoredString(cpuGraph, color.FgGreen))
		output.WriteString("\n\n")

		// Memory graph
		memGraph := plotPodGraph(pod, "Stats.Memory", "Memory %", viewWidth-10, 0, 100)
		output.WriteString(utils.ColoredString(memGraph, color.FgBlue))
	}

	return output.String(), nil
}

// plotPodGraph plots a graph for pod statistics
func plotPodGraph(pod *commands.Pod, statPath, caption string, width int, min, max float64) string {
	pod.StatsMutex.Lock()
	defer pod.StatsMutex.Unlock()

	if len(pod.StatHistory) == 0 {
		return ""
	}

	data := make([]float64, len(pod.StatHistory))

	for i, stats := range pod.StatHistory {
		value, err := lookup.LookupString(stats, statPath)
		if err != nil {
			continue
		}
		floatValue, err := getFloat(value.Interface())
		if err != nil {
			continue
		}
		data[i] = floatValue
	}

	captionStr := fmt.Sprintf(
		"%s: %0.2f (%v)",
		caption,
		data[len(data)-1],
		time.Since(pod.StatHistory[0].RecordedAt).Round(time.Second),
	)

	return asciigraph.Plot(
		data,
		asciigraph.Height(10),
		asciigraph.Width(width),
		asciigraph.Min(min),
		asciigraph.Max(max),
		asciigraph.Caption(captionStr),
	)
}

// from Dave C's answer at https://stackoverflow.com/questions/20767724/converting-unknown-interface-to-float64-in-golang
func getFloat(unk interface{}) (float64, error) {
	floatType := reflect.TypeOf(float64(0))
	stringType := reflect.TypeOf("")

	switch i := unk.(type) {
	case float64:
		return i, nil
	case float32:
		return float64(i), nil
	case int64:
		return float64(i), nil
	case int32:
		return float64(i), nil
	case int:
		return float64(i), nil
	case uint64:
		return float64(i), nil
	case uint32:
		return float64(i), nil
	case uint:
		return float64(i), nil
	case string:
		return strconv.ParseFloat(i, 64)
	default:
		v := reflect.ValueOf(unk)
		v = reflect.Indirect(v)
		if v.Type().ConvertibleTo(floatType) {
			fv := v.Convert(floatType)
			return fv.Float(), nil
		} else if v.Type().ConvertibleTo(stringType) {
			sv := v.Convert(stringType)
			s := sv.String()
			return strconv.ParseFloat(s, 64)
		} else {
			return math.NaN(), fmt.Errorf("Can't convert %v to float64", v.Type())
		}
	}
}
