#!/bin/sh

while true
do
  echo $((1 + $RANDOM % 10))
  sleep 1
done
