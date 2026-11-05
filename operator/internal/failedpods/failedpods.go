// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package failedpods decides when an outdated broker pod that cannot start
// may be replaced without the rolling-restart gates, which fail closed on a
// down broker. A broker that isn't running serves nothing, so replacing its
// pod takes nothing down; what needs guarding is moving pods onto a template
// that is already failing, or onto one that a previous replacement hasn't
// shown to work yet.
//
// A broker may answer once its redpanda container runs and has started, unless
// its pod is stuck terminating on a node that's gone. It is up only then, and,
// while another broker may answer, only on their word: pod status lags while
// kubelet pulls images and runs PostStart hooks, and a broker never reports
// itself down. With no other broker that may answer, its container decides.
// With others, but none answering, as when the operator can't reach them,
// nothing is known: nothing is up, and nothing is replaced. Who may answer is
// known by pod name, as answers are, so a pod's later status changes only what
// its own container says, and a broker that may answer in a later list but
// wasn't asked holds the replacement.
package failedpods

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

// askTimeout outlasts Redpanda's 5s health-overview deadline, past which it
// answers from its last snapshot.
const askTimeout = 10 * time.Second

// terminationSlack is how long past its deletion deadline kubelet may take to
// remove a pod. One listed longer is stuck on a node that won't finish it: no
// deletion is in flight, and its broker says nothing, about its template or
// its peers.
const terminationSlack = time.Minute

// Pod is a broker pod as one roll path sees it.
type Pod struct {
	*corev1.Pod
	// Pool groups pods that share a template; Current marks a pod on its
	// pool's current one.
	Pool    string
	Current bool
	// BrokerID is nil when not yet known: the pod then stands for every broker
	// the answers' member lists register at its address, which a broker that
	// lost its disk shares with its old ID, and one that never joined for none.
	BrokerID *int
	// Removing marks a pod a scale-down is removing: replacing it gains
	// nothing, and its broker says nothing about its template.
	Removing bool
}

// Answer is one broker's view of the cluster: its health overview, and its
// member list, by which pods map to brokers.
type Answer struct {
	Health  rpadmin.ClusterHealthOverview
	Brokers []rpadmin.Broker
}

// Ask returns pod's broker's Answer. AskPeers calls it concurrently.
type Ask func(ctx context.Context, pod *corev1.Pod) (Answer, error)

// Peers is what the brokers that answered report about each other.
type Peers struct {
	reports map[string]report
	// voices names the pods asked about whose broker may answer, true for a
	// name another pod shares; unseen marks brokers outside them.
	voices map[string]bool
	unseen bool
	// brokers is every member any answer lists.
	brokers []rpadmin.Broker
}

type report struct{ alive, down []int }

// Candidate reports whether p is outdated, cannot start, isn't being removed,
// keeps its data across a replacement, and no other broker sees its broker
// alive, nor fails to answer whether it does: its status may be stale. A start
// that hung counts only on their word: alone, a kubelet restart looks the same.
// And while others answer, one that can't be scheduled must be a member's: a
// non-member's keeps no gate failing, so the gated roll replaces it, where its
// replacement, likely as unschedulable, would hold that roll.
func Candidate(p Pod, peers Peers) bool {
	others := peers.othersMayAnswer(p)
	return MayBeCandidate(p) && !peers.SeenAlive(p) && !peers.unheard(p) &&
		(failingToStart(p) || others) && (!others || !Unscheduled(p) || len(peers.brokerIDs(p)) > 0)
}

// MayBeCandidate reports whether p is a Candidate if its peers allow it.
func MayBeCandidate(p Pod) bool {
	return !p.Current && !p.Removing && p.DeletionTimestamp == nil && persistentDataDir(p.Pod) &&
		(failingToStart(p) || utils.ContainerHung(p.Pod, redpandachart.RedpandaContainerName))
}

// Unscheduled reports whether p's pod has yet to be scheduled.
func Unscheduled(p Pod) bool {
	return unschedulable(p.Pod)
}

// Waits reports whether candidate, if it can't be scheduled, waits for
// another of its pool's outdated pods that can be and fails to start:
// replacing that one shows the template runs, where candidate's replacement
// can't, and would hold the pool. A hung start isn't waited for: it may wait
// on the cluster, on candidate even. replaceable limits those to the ones the
// caller would replace.
func Waits(pods []Pod, candidate Pod, replaceable func(Pod) bool) bool {
	return Unscheduled(candidate) && slices.ContainsFunc(pods, func(q Pod) bool {
		return q.Pool == candidate.Pool && !q.Current && !q.Removing && q.DeletionTimestamp == nil && persistentDataDir(q.Pod) &&
			!Unscheduled(q) && replaceable(q) && failing(q)
	})
}

func failingToStart(p Pod) bool {
	return utils.ContainerFailingToStart(p.Pod, redpandachart.RedpandaContainerName)
}

// failing reports whether p's broker fails to start whatever the cluster's
// state, as a crash loop does through every phase of its cycle.
func failing(p Pod) bool {
	cs := redpandaStatus(p)
	return failingToStart(p) || cs != nil && utils.CrashedRecently(*cs)
}

// Hold returns why candidate may not be replaced yet, or "" if it may. It
// holds while a pod is terminating, so replacements go one at a time; while a
// broker that may answer wasn't asked, as one that started since its peers
// were; and while candidate's pool's current template has a broker that isn't
// up and none that is: either that template is failing, or a replacement onto
// it hasn't rejoined yet. A single up broker shows the template runs, so the
// others being down is theirs to fix, unless one can't be scheduled: then the
// template doesn't fit every node, and the candidate's may be one it doesn't.
// Only a member's broker shows the template runs. One a member's answer shows
// never joined counts only if it fails to start: waiting to join, it may wait
// on the very brokers held.
func Hold(pods []Pod, candidate Pod, peers Peers) string {
	membersAnswered := slices.ContainsFunc(pods, func(q Pod) bool {
		_, answered := peers.reports[q.Name]
		return answered && len(peers.brokerIDs(q)) > 0
	})
	var up, down int
	for _, p := range pods {
		if stuck(p) {
			continue
		}
		if p.DeletionTimestamp != nil {
			return fmt.Sprintf("pod %s is terminating", p.Name)
		}
		if _, asked := peers.voices[p.Name]; mayAnswer(p) && !asked {
			return fmt.Sprintf("pod %s's broker started since its peers were asked", p.Name)
		}
		if p.Pool != candidate.Pool || !p.Current || p.Removing {
			continue
		}
		if unschedulable(p.Pod) {
			return fmt.Sprintf("pod %s on pool %q's current template can't be scheduled", p.Name, candidate.Pool)
		}
		member := len(peers.brokerIDs(p)) > 0
		switch {
		case !member && membersAnswered && !failing(p):
		case member && peers.Up(p):
			up++
		default:
			down++
		}
	}
	if down > 0 && up == 0 {
		return fmt.Sprintf("no broker on pool %q's current template is up", candidate.Pool)
	}
	return ""
}

// AskPeers asks every pod whose broker may answer for its Answer, in parallel.
// Each answer stands alone, so a broker that doesn't answer removes only its
// own. Answers are known by pod name, so a name two pods share, as a
// StretchCluster's clusters may, is asked of neither, and both count as brokers
// that didn't answer. unseen reports that brokers may run outside pods, as in a
// cluster the operator can't reach; they count the same way.
func AskPeers(ctx context.Context, pods []Pod, unseen bool, ask Ask) Peers {
	peers := Peers{reports: map[string]report{}, voices: map[string]bool{}, unseen: unseen}
	names := map[string]int{}
	for _, p := range pods {
		names[p.Name]++
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range pods {
		if !mayAnswer(p) {
			continue
		}
		peers.voices[p.Name] = names[p.Name] > 1
		if names[p.Name] > 1 {
			continue
		}
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, askTimeout)
			defer cancel()
			a, err := ask(ctx, p.Pod)
			if err != nil {
				return
			}
			alive := slices.DeleteFunc(slices.Clone(a.Health.AllNodes), func(id int) bool { return slices.Contains(a.Health.NodesDown, id) })
			mu.Lock()
			defer mu.Unlock()
			peers.reports[p.Name] = report{alive: alive, down: a.Health.NodesDown}
			for _, b := range a.Brokers {
				if !slices.ContainsFunc(peers.brokers, func(o rpadmin.Broker) bool {
					return o.NodeID == b.NodeID && o.InternalRPCAddress == b.InternalRPCAddress
				}) {
					peers.brokers = append(peers.brokers, b)
				}
			}
		})
	}
	wg.Wait()
	return peers
}

// Up reports whether p's broker is up: it may answer, isn't fresh off a
// short-lived run, and, if another broker may answer, another that answered
// reports it alive and none reports it down. p may be listed later than the
// pods AskPeers was given, as for every method.
func (peers Peers) Up(p Pod) bool {
	if cs := redpandaStatus(p); !mayAnswer(p) || utils.CrashedRecently(*cs) {
		return false
	}
	if !peers.othersMayAnswer(p) {
		return true
	}
	alive, down := peers.reported(p)
	return alive > 0 && down == 0
}

// SeenAlive reports whether another broker that answered reports p's broker
// alive.
func (peers Peers) SeenAlive(p Pod) bool {
	alive, _ := peers.reported(p)
	return alive > 0
}

// AnotherDown reports whether a broker besides p's is down: what p's
// readiness, which is cluster health, waits on. Another broker that answered
// must report it, or, with no other broker that may answer, p's own: alone,
// its view is what its readiness reads.
func (peers Peers) AnotherDown(p Pod) bool {
	ids := peers.brokerIDs(p)
	another := func(r report) bool {
		return slices.ContainsFunc(r.down, func(down int) bool { return !slices.Contains(ids, down) })
	}
	if !peers.othersMayAnswer(p) {
		r, ok := peers.reports[p.Name]
		return ok && another(r)
	}
	if len(ids) == 0 {
		return false
	}
	for name, r := range peers.reports {
		if name != p.Name && another(r) {
			return true
		}
	}
	return false
}

// unheard reports whether other brokers may answer but none did.
func (peers Peers) unheard(p Pod) bool {
	for name := range peers.reports {
		if name != p.Name {
			return false
		}
	}
	return peers.othersMayAnswer(p)
}

// othersMayAnswer reports whether a broker besides p's may answer: one under
// another name, or under p's when another pod shares it, as the rule can't
// tell them apart.
func (peers Peers) othersMayAnswer(p Pod) bool {
	if peers.unseen {
		return true
	}
	for name, shared := range peers.voices {
		if name != p.Name || shared {
			return true
		}
	}
	return false
}

// reported counts the other brokers that report one of p's brokers alive,
// and down: seen alive if any is, up only if none is down.
func (peers Peers) reported(p Pod) (alive, down int) {
	ids := peers.brokerIDs(p)
	for name, r := range peers.reports {
		if name == p.Name {
			continue
		}
		if slices.ContainsFunc(ids, func(id int) bool { return slices.Contains(r.alive, id) }) {
			alive++
		}
		if slices.ContainsFunc(ids, func(id int) bool { return slices.Contains(r.down, id) }) {
			down++
		}
	}
	return alive, down
}

func (peers Peers) brokerIDs(p Pod) []int {
	if p.BrokerID != nil {
		return []int{*p.BrokerID}
	}
	return registeredAt(peers.brokers, p.Pod)
}

// BrokerID returns the one broker whose internal RPC address names pod, or
// nil when none or more than one does.
func BrokerID(brokers []rpadmin.Broker, pod *corev1.Pod) *int {
	if ids := registeredAt(brokers, pod); len(ids) == 1 {
		return &ids[0]
	}
	return nil
}

// registeredAt returns the brokers whose internal RPC address names pod, by
// pod name or bare IP.
func registeredAt(brokers []rpadmin.Broker, pod *corev1.Pod) []int {
	var ids []int
	for _, b := range brokers {
		host := b.InternalRPCAddress
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host == "" || (strings.SplitN(host, ".", 2)[0] != pod.Name && host != pod.Status.PodIP) || slices.Contains(ids, b.NodeID) {
			continue
		}
		ids = append(ids, b.NodeID)
	}
	return ids
}

// mayAnswer reports whether p's broker may answer: its redpanda container runs
// and has started, as a startup probe passing or, without one, a PostStart
// hook returning marks.
func mayAnswer(p Pod) bool {
	cs := redpandaStatus(p)
	return cs != nil && cs.State.Running != nil && ptr.Deref(cs.Started, false) && !stuck(p)
}

// stuck reports whether p is terminating longer than kubelet takes, as on a
// node that's gone, whose last status still reads running.
func stuck(p Pod) bool {
	return p.DeletionTimestamp != nil && time.Since(p.DeletionTimestamp.Time) > terminationSlack
}

// Unchanged is the delete precondition that pod is still as it was judged: a
// status write since, as kubelet's once its container starts, fails it.
func Unchanged(pod *corev1.Pod) client.Preconditions {
	var p client.Preconditions
	if pod.UID != "" {
		p.UID = &pod.UID
	}
	if pod.ResourceVersion != "" {
		p.ResourceVersion = &pod.ResourceVersion
	}
	return p
}

// unschedulable reports whether pod can't be scheduled. A bound pod can't
// be, whatever a condition written back from a stale read says.
func unschedulable(pod *corev1.Pod) bool {
	return pod.Spec.NodeName == "" && slices.ContainsFunc(pod.Status.Conditions, func(c corev1.PodCondition) bool {
		return c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse && c.Reason == corev1.PodReasonUnschedulable
	})
}

func redpandaStatus(p Pod) *corev1.ContainerStatus {
	i := slices.IndexFunc(p.Status.ContainerStatuses, func(cs corev1.ContainerStatus) bool {
		return cs.Name == redpandachart.RedpandaContainerName
	})
	if i < 0 {
		return nil
	}
	return &p.Status.ContainerStatuses[i]
}

func persistentDataDir(pod *corev1.Pod) bool {
	return slices.ContainsFunc(pod.Spec.Volumes, func(v corev1.Volume) bool {
		return v.Name == redpanda.DatadirVolumeName && (v.PersistentVolumeClaim != nil || v.HostPath != nil)
	})
}
