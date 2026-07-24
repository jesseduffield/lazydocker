package presentation

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConvertBigMetricFromSchema(t *testing.T) {
	type scenario struct {
		name        string
		data        map[string]interface{}
		expected    map[string]interface{}
		expectedErr string
	}

	inJSONBytes, err := os.ReadFile("testdata/stats.json")
	if err != nil {
		t.Fatalf("failed to read testdata/stats.json: %v", err)
	}
	loadInJSON := func() map[string]interface{} {
		var m map[string]interface{}
		if err := json.Unmarshal(inJSONBytes, &m); err != nil {
			t.Fatalf("failed to unmarshal testdata/stats.json: %v", err)
		}
		return m
	}
	setNested := func(m map[string]interface{}, keys []string, val interface{}) {
		for _, k := range keys[:len(keys)-1] {
			m = m[k].(map[string]interface{})
		}
		m[keys[len(keys)-1]] = val
	}

	inJSONExpected := loadInJSON()
	setNested(inJSONExpected, []string{"ClientStats", "memory_stats", "limit"}, "1.911GB")
	setNested(inJSONExpected, []string{"ClientStats", "memory_stats", "stats", "hierarchical_memory_limit"}, "0")
	setNested(inJSONExpected, []string{"ClientStats", "memory_stats", "stats", "hierarchical_memsw_limit"}, "0")
	setNested(inJSONExpected, []string{"ClientStats", "cpu_stats", "cpu_usage", "total_usage"}, "30.283ms")
	setNested(inJSONExpected, []string{"ClientStats", "cpu_stats", "cpu_usage", "usage_in_kernelmode"}, "15.664ms")
	setNested(inJSONExpected, []string{"ClientStats", "cpu_stats", "cpu_usage", "usage_in_usermode"}, "14.619ms")
	setNested(inJSONExpected, []string{"ClientStats", "cpu_stats", "system_cpu_usage"}, "25.994h")
	setNested(inJSONExpected, []string{"ClientStats", "precpu_stats", "cpu_usage", "total_usage"}, "30.283ms")
	setNested(inJSONExpected, []string{"ClientStats", "precpu_stats", "cpu_usage", "usage_in_kernelmode"}, "15.664ms")
	setNested(inJSONExpected, []string{"ClientStats", "precpu_stats", "cpu_usage", "usage_in_usermode"}, "14.619ms")
	setNested(inJSONExpected, []string{"ClientStats", "precpu_stats", "system_cpu_usage"}, "25.994h")

	scenarios := []scenario{
		{
			name: "string schema: converts float64 bytes",
			data: map[string]interface{}{
				"ClientStats": map[string]interface{}{
					"memory_stats": map[string]interface{}{
						"limit": float64(2048),
					},
				},
			},
			expected: map[string]interface{}{
				"ClientStats": map[string]interface{}{
					"memory_stats": map[string]interface{}{
						"limit": "2.000KB",
					},
				},
			},
		},
		{
			name: "string schema: converts int64 nanoseconds",
			data: map[string]interface{}{
				"ClientStats": map[string]interface{}{
					"cpu_stats": map[string]interface{}{
						"cpu_usage": map[string]interface{}{
							"total_usage":         float64(100000000000),
							"percpu_usage":        []float64{50000000000, 50000000000},
							"usage_in_kernelmode": float64(200000),
							"usage_in_usermode":   float64(200000),
						},
						"system_cpu_usage": float64(100000000000),
					},
				},
			},
			expected: map[string]interface{}{
				"ClientStats": map[string]interface{}{
					"cpu_stats": map[string]interface{}{
						"cpu_usage": map[string]interface{}{
							"total_usage":         "100.000s",
							"percpu_usage":        []string{"50.000s", "50.000s"},
							"usage_in_kernelmode": "200.000µs",
							"usage_in_usermode":   "200.000µs",
						},
						"system_cpu_usage": "100.000s",
					},
				},
			},
		},
		{
			name:     "empty data is a no-op",
			data:     map[string]interface{}{},
			expected: map[string]interface{}{},
		},
		{
			name: "bytes below unit threshold stays as raw number",
			data: map[string]interface{}{
				"ClientStats": map[string]interface{}{
					"memory_stats": map[string]interface{}{
						"limit": float64(500),
					},
				},
			},
			expected: map[string]interface{}{
				"ClientStats": map[string]interface{}{
					"memory_stats": map[string]interface{}{
						"limit": "500",
					},
				},
			},
		},
		{
			name: "bytes at MB scale",
			data: map[string]interface{}{
				"ClientStats": map[string]interface{}{
					"memory_stats": map[string]interface{}{
						"limit": float64(1500000),
					},
				},
			},
			expected: map[string]interface{}{
				"ClientStats": map[string]interface{}{
					"memory_stats": map[string]interface{}{
						"limit": "1.431MB",
					},
				},
			},
		},
		{
			name: "converts deeply nested hierarchical memory limits",
			data: map[string]interface{}{
				"ClientStats": map[string]interface{}{
					"memory_stats": map[string]interface{}{
						"stats": map[string]interface{}{
							"hierarchical_memory_limit": float64(1073741824),
							"hierarchical_memsw_limit":  float64(2147483648),
						},
					},
				},
			},
			expected: map[string]interface{}{
				"ClientStats": map[string]interface{}{
					"memory_stats": map[string]interface{}{
						"stats": map[string]interface{}{
							"hierarchical_memory_limit": "1.000GB",
							"hierarchical_memsw_limit":  "2.000GB",
						},
					},
				},
			},
		},
		{
			name: "converts precpu_stats branch",
			data: map[string]interface{}{
				"ClientStats": map[string]interface{}{
					"precpu_stats": map[string]interface{}{
						"cpu_usage": map[string]interface{}{
							"total_usage":  float64(1500000),
							"percpu_usage": []float64{1000, 2000000000},
						},
						"system_cpu_usage": float64(1500000000000),
					},
				},
			},
			expected: map[string]interface{}{
				"ClientStats": map[string]interface{}{
					"precpu_stats": map[string]interface{}{
						"cpu_usage": map[string]interface{}{
							"total_usage":  "1.500ms",
							"percpu_usage": []string{"1.000µs", "2.000s"},
						},
						"system_cpu_usage": "1.500m",
					},
				},
			},
		},
		{
			name: "preserves fields not listed in PATHS_TO_CONVERT_BIGMETRICS",
			data: map[string]interface{}{
				"ClientStats": map[string]interface{}{
					"memory_stats": map[string]interface{}{
						"limit": float64(2048),
						"usage": float64(1024),
					},
					"name": "my-container",
				},
			},
			expected: map[string]interface{}{
				"ClientStats": map[string]interface{}{
					"memory_stats": map[string]interface{}{
						"limit": "2.000KB",
						"usage": float64(1024),
					},
					"name": "my-container",
				},
			},
		},
		{
			name:     "converts realistic docker stats loaded from testdata/stats.json",
			data:     loadInJSON(),
			expected: inJSONExpected,
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			data := s.data
			err := convertBigMetric(&data)
			if s.expectedErr != "" {
				assert.EqualError(t, err, s.expectedErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, s.expected, data)
		})
	}
}
