#!/usr/bin/env bash

set -e

echo "Compare rpk profile"
actualProfile=$(kubectl exec --namespace $NAMESPACE pod/centralized-configuration-0 -- rpk profile print)
expectedProfile=$(cat rpk-profile.txt)
diff -b <(echo "$actualProfile") <(echo "$expectedProfile")

echo "Compare broker list"
actualBrokerList=$(kubectl exec --namespace $NAMESPACE pod/centralized-configuration-0 -- rpk redpanda admin brokers list | awk '{print $1, $2}')
expectedBrokerList=$(cat broker-list.txt)
diff -b <(echo "$actualBrokerList") <(echo "$expectedBrokerList")

echo "Compare schema list"
# Schema if it's empty the output will give nothing. There is no reason to compare 2 empty strings
kubectl exec --namespace $NAMESPACE pod/centralized-configuration-0 -- rpk registry schema list

echo "Compare topic list"
kubectl exec --namespace $NAMESPACE pod/centralized-configuration-0 -- rpk topic create test
actualTopicList=$(kubectl exec --namespace $NAMESPACE pod/centralized-configuration-0 -- rpk topic list)
kubectl exec --namespace $NAMESPACE pod/centralized-configuration-0 -- rpk topic delete test
expectedTopicList=$(cat topic-list.txt)
diff -b <(echo "$actualTopicList") <(echo "$expectedTopicList")