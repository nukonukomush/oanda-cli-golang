build:
	go build -o bin/oanda

alias b := build

test: build
	cargo test --manifest-path=spec/Cargo.toml

alias t := test

install: build
	go install oanda.go
