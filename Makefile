# This Makefile is meant to be used by people that do not usually work
# with Go source code. If you know what GOPATH is then you probably
# don't need to bother with make.

.PHONY: all test clean

build:
	go build

test: all
	go test -v ./...

ps:
	ps aux | grep HayekPool

start:
	go build
	nohup ./HayekPool hayekconfig.json &

run:
	go build
	./HayekPool hayekconfig.json

nginx-reload:
	sudo nginx -s reload

clean:
	-rm HayekPool nohup.out
