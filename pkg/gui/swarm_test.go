package gui

import (
	"bytes"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestSwarmLogWriter(t *testing.T) {
	scenarios := []struct {
		name       string
		timestamps bool
		noLabel    bool
		writes     []string
		expected   string
	}{
		{
			name: "prefixes each line with its task, across partial writes",
			writes: []string{
				"com.docker.swarm.node.id=n1,com.docker.swarm.task.id=t1 first\ncom.docker.swarm.node.id=n1,com.docker.swarm.task.id=t2 sec",
				"ond\n",
			},
			expected: "t1@n1 | first\nt2@n1 | second\n",
		},
		{
			name:       "keeps timestamps in front",
			timestamps: true,
			writes:     []string{"2024-01-01T00:00:00Z com.docker.swarm.node.id=n1,com.docker.swarm.task.id=t1 hi\n"},
			expected:   "2024-01-01T00:00:00Z t1@n1 | hi\n",
		},
		{
			name:     "strips details without prefixing",
			noLabel:  true,
			writes:   []string{"com.docker.swarm.node.id=n1,com.docker.swarm.task.id=t1 hi\n"},
			expected: "hi\n",
		},
		{
			name:     "flushes a trailing line without newline",
			writes:   []string{"no details"},
			expected: "no details\n",
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			writer := &swarmLogWriter{out: out, timestamps: s.timestamps}
			if !s.noLabel {
				writer.label = func(taskID, nodeID string) string { return taskID + "@" + nodeID }
			}
			for _, w := range s.writes {
				n, err := writer.Write([]byte(w))
				assert.NoError(t, err)
				assert.Equal(t, len(w), n)
			}
			writer.Flush()

			assert.Equal(t, s.expected, utils.Decolorise(out.String()))
		})
	}
}

func TestSwarmNodeInfo(t *testing.T) {
	node := &commands.SwarmNode{ID: "n1", Node: swarm.Node{
		Spec: swarm.NodeSpec{Annotations: swarm.Annotations{Labels: map[string]string{"zone": "a", "disk": "ssd"}}},
		Description: swarm.NodeDescription{
			Platform:  swarm.Platform{OS: "linux", Architecture: "x86_64"},
			Resources: swarm.Resources{NanoCPUs: 1500000000, MemoryBytes: 2 << 30},
		},
		Status:        swarm.NodeStatus{Addr: "10.0.0.2"},
		ManagerStatus: &swarm.ManagerStatus{Reachability: swarm.ReachabilityUnreachable, Addr: "10.0.0.2:2377"},
	}}

	// not a raw string: the "Labels: " line ends in padding that editors strip
	expected := "Address:        10.0.0.2\n" +
		"Platform:       linux/x86_64\n" +
		"CPUs:           1.5\n" +
		"Memory:         2.00GiB\n" +
		"Manager:        unreachable, 10.0.0.2:2377\n" +
		"Labels:         \n" +
		"                disk: ssd\n" +
		"                zone: a\n" +
		"Engine Labels:  none\n"
	assert.Equal(t, expected, utils.Decolorise(swarmNodeInfo(node)))
}
