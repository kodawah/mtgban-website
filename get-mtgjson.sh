#!/bin/bash

set -e

curl "https://mtgjson.com/api/v5/AllPrintings.json" > /tmp/allprintings5.json
mv /tmp/allprintings5.json .
