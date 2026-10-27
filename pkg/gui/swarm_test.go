package gui

import (
	"bytes"
	"testing"

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
