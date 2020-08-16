#!/bin/bash

set -e

curl "https://www.mtgjson.com/files/AllCards.json" > /tmp/allcards.json
mv /tmp/allcards.json .

curl "https://www.mtgjson.com/files/AllPrintings.json" > /tmp/allprintings.json
mv /tmp/allprintings.json .

curl "https://mtgjson.com/api/v5/AllPrintings.json" > /tmp/allprintings5.json
mv /tmp/allprintings5.json .
