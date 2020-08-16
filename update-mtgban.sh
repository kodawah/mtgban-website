#!/bin/bash

TIP=$(git ls-remote https://github.com/kodabb/go-mtgban.git HEAD | awk '{ print $1}')

GOSUMDB=off go get -u github.com/kodabb/go-mtgban@$TIP

TIP=$(git ls-remote https://github.com/kodabb/go-mtgmatcher.git HEAD | awk '{ print $1}')

GOSUMDB=off go get -u github.com/kodabb/go-mtgmatcher@$TIP

go mod tidy
