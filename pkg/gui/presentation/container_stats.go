package presentation

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/jesseduffield/asciigraph"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/utils"
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
