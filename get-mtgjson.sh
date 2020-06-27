#!/bin/bash

set -e

curl "https://www.mtgjson.com/files/AllCards.json" > /tmp/allcards.json
mv /tmp/allcards.json .

curl "https://www.mtgjson.com/files/AllPrintings.json" > /tmp/allprintings.json
mv /tmp/allprintings.json .
