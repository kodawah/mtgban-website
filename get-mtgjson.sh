#!/bin/bash

curl -O "https://mtgjson.com/api/v5/AllPrintings.json.xz"

xz -dc AllPrintings.json.xz | jq > /tmp/allprintings5.json.new

if [[ $? == 0 ]]
then
    mv /tmp/allprintings5.json.new ./allprintings5.json
fi

rm AllPrintings.json.xz
