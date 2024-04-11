This test

0. creates a 3 node Redpanda cluster
1. enables decommission on pod deletion
2. deletes one pod at a time and checks that its node id increases as expected
3. disables decommision on pod deletion
4. deletes a pod and checks that its node id did not change