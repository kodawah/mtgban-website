#!/bin/bash

TIP=$(git ls-remote https://github.com/kodabb/go-mtgban.git HEAD | awk '{ print $1}')

go get -u github.com/kodabb/go-mtgban@$TIP
go mod tidy
go mod vendor
