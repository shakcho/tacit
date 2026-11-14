.PHONY: bootstrap verify test check-boundaries generated

bootstrap:
	./scripts/bootstrap.sh

verify:
	./scripts/verify.sh

test:
	go test ./...
	pnpm --recursive test
	PYTHONPATH=services/speech/src uv run --all-packages python -m unittest discover -s services/speech/tests

check-boundaries:
	node scripts/check-boundaries.mjs

generated:
	./scripts/check-generated.sh
