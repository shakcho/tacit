package memory

import (
	"context"
	"sync"

	"github.com/shakcho/tacit/internal/runtime/shellsample"
)

type Limits struct {
	Samples            int
	IdempotencyEntries int
}

type recordedCommand struct {
	command shellsample.CreateCommand
	result  shellsample.CreateResult
}

type ShellSamples struct {
	mu       sync.Mutex
	limits   Limits
	samples  map[shellsample.ID]shellsample.Sample
	commands map[shellsample.IdempotencyKey]recordedCommand
}

var _ shellsample.Samples = (*ShellSamples)(nil)

func NewShellSamples(limits Limits) *ShellSamples {
	return &ShellSamples{
		limits:   limits,
		samples:  make(map[shellsample.ID]shellsample.Sample),
		commands: make(map[shellsample.IdempotencyKey]recordedCommand),
	}
}

func (samples *ShellSamples) Create(ctx context.Context, command shellsample.CreateCommand) (shellsample.CreateResult, error) {
	if err := ctx.Err(); err != nil {
		return shellsample.NewUnavailable(), err
	}
	samples.mu.Lock()
	defer samples.mu.Unlock()

	if prior, ok := samples.commands[command.Key]; ok {
		if prior.command != command {
			return shellsample.NewIdempotencyConflict(), nil
		}
		if sample, ok := prior.result.Sample(); ok {
			return shellsample.NewReplayed(sample), nil
		}
		return prior.result, nil
	}
	if len(samples.commands) >= samples.limits.IdempotencyEntries {
		return shellsample.NewUnavailable(), nil
	}
	sample, exists := samples.samples[command.ID]
	if exists && sample.Offset != command.Offset {
		result := shellsample.NewSampleConflict()
		samples.commands[command.Key] = recordedCommand{command: command, result: result}
		return result, nil
	}
	if !exists {
		if len(samples.samples) >= samples.limits.Samples {
			return shellsample.NewUnavailable(), nil
		}
		sample = shellsample.Sample{ID: command.ID, Offset: command.Offset}
		samples.samples[sample.ID] = sample
	}
	result := shellsample.NewCreated(sample)
	samples.commands[command.Key] = recordedCommand{command: command, result: result}
	return result, nil
}

func (samples *ShellSamples) Find(ctx context.Context, id shellsample.ID) (shellsample.Sample, bool, error) {
	if err := ctx.Err(); err != nil {
		return shellsample.Sample{}, false, err
	}
	samples.mu.Lock()
	defer samples.mu.Unlock()
	sample, ok := samples.samples[id]
	return sample, ok, nil
}
