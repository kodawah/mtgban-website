#!/bin/bash

TIP=$(git ls-remote https://github.com/mtgban/go-mtgban.git HEAD | awk '{ print $1}')

GOSUMDB=off go get -u github.com/mtgban/go-mtgban@$TIP

go mod tidy
