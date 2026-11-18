package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
)

const (
	valueCommandLimit  = 8 << 20
	valueRetainedLimit = 32 << 20
)

type valueLimitedBuffer struct {
	data     bytes.Buffer
	limit    int
	exceeded bool
}

func (b *valueLimitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.data.Len() {
		b.exceeded = true
		return 0, errors.New("value command output exceeds limit")
	}
	return b.data.Write(p)
}

// Bound both pipes while the child is running, before parsing any repository data.
func valueCommandOutput(cmd *exec.Cmd, limit int) ([]byte, error) {
	stdout, stderr := valueLimitedBuffer{limit: limit}, valueLimitedBuffer{limit: 16 << 10}
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if stdout.exceeded || stderr.exceeded {
		return nil, fmt.Errorf("value command exceeded the output limit (%d bytes stdout, %d bytes stderr)", limit, stderr.limit)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		exit.Stderr = stderr.data.Bytes()
	}
	if err != nil {
		return nil, err
	}
	return stdout.data.Bytes(), nil
}

type valueEvidenceBudget struct{ remaining int }

func (b *valueEvidenceBudget) retain(fixed int, lists ...[]string) error {
	if b == nil {
		return nil
	}
	size := fixed
	for _, list := range lists {
		size += 16 * len(list) // String headers, including short or empty entries.
		for _, s := range list {
			size += len(s)
		}
	}
	if size > b.remaining {
		return errors.New("value report exceeds the retained evidence budget; shorten the period or reduce the measurement population")
	}
	b.remaining -= size
	return nil
}

func (g valueGit) measure(m valueMeasurement, sha string) (valueSnapshot, error) {
	s, err := m.measure(g, sha)
	if err != nil {
		return valueSnapshot{}, err
	}
	var population, fields, missing []string
	if s.Population != nil {
		population = *s.Population
	}
	if s.Coverage != nil {
		fields, missing = s.Coverage.Fields, s.Coverage.Missing
	}
	if err := g.budget.retain(256, population, fields, missing); err != nil {
		return valueSnapshot{}, err
	}
	return s, nil
}
