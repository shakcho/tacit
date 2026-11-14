package memory_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/shakcho/tacit/internal/platform/memory"
	"github.com/shakcho/tacit/internal/runtime/shellsample"
)

func command(t *testing.T, idByte byte, offset, key string) shellsample.CreateCommand {
	t.Helper()
	id, err := shellsample.ParseID(fmt.Sprintf("smp_%032x", idByte))
	if err != nil {
		t.Fatal(err)
	}
	mediaTime, err := shellsample.ParseMediaTime(offset)
	if err != nil {
		t.Fatal(err)
	}
	idempotencyKey, err := shellsample.ParseIdempotencyKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return shellsample.CreateCommand{ID: id, Offset: mediaTime, Key: idempotencyKey}
}

func TestCreateFindAndRetry(t *testing.T) {
	ctx := context.Background()
	samples := memory.NewShellSamples(memory.Limits{Samples: 2, IdempotencyEntries: 8})
	first := command(t, 1, "1234567", "first")
	for _, test := range []struct {
		command shellsample.CreateCommand
		outcome shellsample.Outcome
	}{
		{first, shellsample.Created},
		{first, shellsample.Replayed},
		{command(t, 2, "1234567", "first"), shellsample.IdempotencyConflict},
		{command(t, 1, "1234568", "first"), shellsample.IdempotencyConflict},
		{command(t, 1, "1234567", "second"), shellsample.Created},
		{command(t, 1, "1234568", "conflict"), shellsample.SampleConflict},
		{command(t, 1, "1234568", "conflict"), shellsample.SampleConflict},
	} {
		result, err := samples.Create(ctx, test.command)
		if err != nil || result.Outcome() != test.outcome {
			t.Fatalf("Create = %v, %v; want %v", result.Outcome(), err, test.outcome)
		}
	}
	sample, found, err := samples.Find(ctx, first.ID)
	if err != nil || !found || sample.ID != first.ID || sample.Offset != first.Offset {
		t.Fatalf("Find = %v, %v, %v", sample, found, err)
	}
	if _, found, err := samples.Find(ctx, shellsample.ID{}); err != nil || found {
		t.Fatalf("missing Find = %v, %v", found, err)
	}
}

func TestCapacityDoesNotEvictRetries(t *testing.T) {
	for _, limits := range []memory.Limits{{Samples: 1, IdempotencyEntries: 2}, {Samples: 2, IdempotencyEntries: 1}} {
		samples := memory.NewShellSamples(limits)
		first := command(t, 1, "0", "first")
		if result, _ := samples.Create(context.Background(), first); result.Outcome() != shellsample.Created {
			t.Fatal("first create failed")
		}
		if result, _ := samples.Create(context.Background(), command(t, 2, "0", "second")); result.Outcome() != shellsample.Unavailable {
			t.Fatal("capacity did not refuse new command")
		}
		if result, _ := samples.Create(context.Background(), first); result.Outcome() != shellsample.Replayed {
			t.Fatal("full adapter lost retry")
		}
		if _, found, _ := samples.Find(context.Background(), command(t, 2, "0", "second").ID); found {
			t.Fatal("refused command inserted a sample")
		}
	}
}

func TestConcurrentRetriesCreateOnce(t *testing.T) {
	samples := memory.NewShellSamples(memory.Limits{Samples: 1, IdempotencyEntries: 1})
	input := command(t, 1, "42", "retry")
	results := make(chan shellsample.Outcome, 64)
	var group sync.WaitGroup
	for range 64 {
		group.Go(func() {
			result, err := samples.Create(context.Background(), input)
			if err != nil {
				t.Error(err)
			}
			results <- result.Outcome()
		})
	}
	group.Wait()
	close(results)
	created := 0
	for outcome := range results {
		switch outcome {
		case shellsample.Created:
			created++
		case shellsample.Replayed:
		default:
			t.Fatalf("retry outcome = %v", outcome)
		}
	}
	if created != 1 {
		t.Fatalf("created %d samples for one command", created)
	}
}

func TestAdaptersAreProcessLocal(t *testing.T) {
	first := memory.NewShellSamples(memory.Limits{Samples: 1, IdempotencyEntries: 1})
	second := memory.NewShellSamples(memory.Limits{Samples: 1, IdempotencyEntries: 1})
	input := command(t, 1, "0", "local")
	if _, err := first.Create(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if _, found, err := second.Find(context.Background(), input.ID); err != nil || found {
		t.Fatalf("second adapter unexpectedly shared state: %v, %v", found, err)
	}
}

func TestCanceledCommandHasNoEffect(t *testing.T) {
	samples := memory.NewShellSamples(memory.Limits{Samples: 1, IdempotencyEntries: 1})
	input := command(t, 1, "0", "canceled")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := samples.Create(ctx, input); err != context.Canceled {
		t.Fatalf("Create error = %v", err)
	}
	if _, found, _ := samples.Find(context.Background(), input.ID); found {
		t.Fatal("canceled command inserted a sample")
	}
}
