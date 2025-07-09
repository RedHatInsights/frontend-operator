#!/usr/bin/env bash

set -ex 

echo "Grabbing frontend.yaml from insights-chrome repo"
rm -f frontend.yml
wget https://raw.githubusercontent.com/RedHatInsights/insights-chrome/refs/heads/master/frontend.yml

echo "Applying frontend yaml"
kubectl apply -f frontend.yml
