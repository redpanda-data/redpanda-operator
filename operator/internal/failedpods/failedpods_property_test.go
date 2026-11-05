// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package failedpods_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"pgregory.net/rapid"

	"github.com/redpanda-data/redpanda-operator/operator/internal/failedpods"
)

// TestRuleProperties checks the rule against random clusters, each verdict
// against an oracle that spells the rule out from what the generator knows of
// the world, not of pod status: which broker each pod is, which can answer
// (one that runs, has started, and whose node is still there), and what each
// answered.
func TestRuleProperties(t *testing.T) {
	t.Run("AsksEachBrokerThatCanAnswerOnce", rapid.MakeCheck(func(t *rapid.T) {
		w := genCluster(t)
		var mu sync.Mutex
		asked := map[int]int{}
		failedpods.AskPeers(context.Background(), w.pods, w.unseen, func(_ context.Context, pod *corev1.Pod) (failedpods.Answer, error) {
			mu.Lock()
			defer mu.Unlock()
			asked[w.index(pod)]++
			return failedpods.Answer{}, nil
		})
		for i := range w.pods {
			want := 0
			if w.askable(i) {
				want = 1
			}
			require.Equal(t, want, asked[i], "%s", w.pods[i].Name)
		}
	}))

	t.Run("UpFollowsTheRule", rapid.MakeCheck(func(t *rapid.T) {
		w := genCluster(t)
		peers := w.ask()
		for i, p := range w.pods {
			require.Equal(t, w.up(i), peers.Up(p), "%s", p.Name)
		}
	}))

	t.Run("AnotherDownFollowsTheRule", rapid.MakeCheck(func(t *rapid.T) {
		w := genCluster(t)
		peers := w.ask()
		for i, p := range w.pods {
			require.Equal(t, w.anotherDown(i), peers.AnotherDown(p), "%s", p.Name)
		}
	}))

	t.Run("CandidateFollowsTheRule", rapid.MakeCheck(func(t *rapid.T) {
		w := genCluster(t)
		peers := w.ask()
		for i, p := range w.pods {
			require.Equal(t, w.candidate(i), failedpods.Candidate(p, peers), "%s", p.Name)
		}
	}))

	t.Run("HoldFollowsTheRule", rapid.MakeCheck(func(t *rapid.T) {
		w := genCluster(t)
		peers := w.ask()
		candidate := rapid.IntRange(0, len(w.pods)-1).Draw(t, "candidate")
		require.Equal(t, w.hold(candidate), failedpods.Hold(w.pods, w.pods[candidate], peers) != "")
	}))

	// Only a start that hung for a run that long is given up on.
	t.Run("NeverReplacesAContainerThatStartedOrIsStarting", rapid.MakeCheck(func(t *rapid.T) {
		w := genCluster(t)
		peers := w.ask()
		for i, p := range w.pods {
			if slices.Contains([]string{"running", "between crashes", "starting"}, w.state[i]) {
				require.False(t, failedpods.Candidate(p, peers), "%s runs", p.Name)
			}
		}
	}))

	// Its status can't tell a hung start from a kubelet restart, which resets
	// Started on a broker that runs.
	t.Run("ARunningContainerIsReplacedOnlyOnItsPeersWord", rapid.MakeCheck(func(t *rapid.T) {
		w := genClusterFor(t, "kubelet restarted, alone", "kubelet restarted", "candidate hung")
		peers := w.ask()
		for i, p := range w.pods {
			if w.runs(i) && failedpods.Candidate(p, peers) {
				require.True(t, w.othersMayAnswer(i) && len(w.answered(i)) > 0 && !w.seenAlive(i), "%s runs, and no peer reports it down", p.Name)
			}
		}
	}))

	// A pass's pools each list pods themselves, after the peers were asked:
	// a pod's later status says only what its own container does, and one
	// that may answer but wasn't asked holds.
	t.Run("ALaterListChangesOnlyAPodsOwnContainer", rapid.MakeCheck(func(t *rapid.T) {
		w := genClusterFor(t, "answered, then crashed", "started since", "answered alone, then kubelet restarted")
		peers := w.ask()
		later := w.later(t)
		for i, p := range later.pods {
			require.Equal(t, later.up(i), peers.Up(p), "%s up", p.Name)
			require.Equal(t, later.candidate(i), failedpods.Candidate(p, peers), "%s a candidate", p.Name)
		}
		candidate := rapid.IntRange(0, len(later.pods)-1).Draw(t, "candidate")
		require.Equal(t, later.hold(candidate), failedpods.Hold(later.pods, later.pods[candidate], peers) != "")
	}))

	// While another broker can answer, or may unseen, a pod's own container
	// decides nothing: only an answer does.
	t.Run("ABrokerThatMayAnswerIsNeverIgnored", rapid.MakeCheck(func(t *rapid.T) {
		w := genCluster(t)
		peers := w.ask()
		for i, p := range w.pods {
			if !w.othersMayAnswer(i) {
				continue
			}
			if peers.Up(p) {
				require.True(t, w.seenAlive(i), "%s is up, though no other broker saw it alive", p.Name)
			}
			if failedpods.Candidate(p, peers) {
				require.NotEmpty(t, w.answered(i), "%s is replaced, though no other broker answered", p.Name)
			}
			if peers.AnotherDown(p) {
				require.NotEmpty(t, w.answered(i), "%s's peers are down, though none answered", p.Name)
			}
		}
	}))

	// Alone, its own view is the one its readiness reads.
	t.Run("ABrokerCannotVouchForItself", rapid.MakeCheck(func(t *rapid.T) {
		w := genCluster(t)
		peers := w.ask()
		for i, p := range w.pods {
			vouching := w.askVouching(i)
			require.Equal(t, peers.Up(p), vouching.Up(p), "%s's own answer decides whether it is up", p.Name)
			require.Equal(t, failedpods.Candidate(p, peers), failedpods.Candidate(p, vouching), "%s's own answer decides whether it is replaced", p.Name)
			if w.othersMayAnswer(i) {
				require.Equal(t, peers.AnotherDown(p), vouching.AnotherDown(p), "%s's own answer decides whether another is down", p.Name)
			}
		}
	}))
}

// cluster is broker pods, by index, as the generator knows them: the broker
// each one is, the state it is in, and what it answers.
type cluster struct {
	pods []failedpods.Pod
	// id is the broker a pod is, when it has joined; ghost is the ID it had
	// before it lost its disk, registered at the same address, and down.
	id, ghost map[int]int
	state     []string
	// gone marks a pod stuck terminating on a node that's gone: its status
	// still reads as state, but its broker is down.
	gone []bool
	// answers maps a pod whose broker answers, if asked, to what it answers.
	answers map[int]answer
	// unseen marks brokers the rule isn't shown, as in a cluster the operator
	// can't reach.
	unseen bool
	// askedOf is the world a later list's peers were asked in, nil if this
	// one's; then is how a decision has its pods change before that list.
	askedOf *cluster
	then    map[int]string
}

type answer struct {
	down []int
	// listed is the pods its member list names, respelled those it names
	// twice, under two spellings of one address.
	listed, respelled []int
}

// "hung" is a broker that never started; "kubelet restarted", one that runs,
// whose Started its kubelet reset: the same status.
var states = []string{
	"running", "starting", "hung", "kubelet restarted", "crash looping", "between crashes", "creating", "backed off",
	"image pull failing", "unschedulable",
}

// genCluster draws up to five broker pods across two pools in any state the
// rule reads, some sharing a name as a StretchCluster's clusters may, each
// pod's broker known up front (as a Broker CR's status records it) or left to
// the answers' member lists, which may each miss some members. Half the time it
// draws a decision instead, so each rule's case comes up.
func genCluster(t *rapid.T) *cluster {
	if rapid.Bool().Draw(t, "decision") {
		return genDecision(t, rapid.SampledFrom(decisionChanges).Draw(t, "change"))
	}
	w := &cluster{id: map[int]int{}, ghost: map[int]int{}, answers: map[int]answer{}}
	for i := range rapid.IntRange(1, 5).Draw(t, "pods") {
		name := fmt.Sprintf("pod-%d", i)
		if i > 0 && rapid.IntRange(0, 5).Draw(t, name+" shares a name") == 0 {
			name = w.pods[i-1].Name
		}
		p := w.add(name, rapid.SampledFrom(states).Draw(t, name+" state"))
		p.Pool = rapid.SampledFrom([]string{"a", "b"}).Draw(t, name+" pool")
		p.Current = rapid.Bool().Draw(t, name+" current")
		p.Removing = rapid.IntRange(0, 5).Draw(t, name+" removing") == 0
		if rapid.IntRange(0, 7).Draw(t, name+" terminating") == 0 {
			*p = terminating(*p)
			if rapid.Bool().Draw(t, name+" node gone") {
				w.goneAt(i)
			}
		}
		switch rapid.IntRange(0, 5).Draw(t, name+" data dir") {
		case 0:
			p.Spec.Volumes = []corev1.Volume{{Name: "datadir", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}
		case 1:
			p.Spec.Volumes = []corev1.Volume{{Name: "datadir", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/mnt/redpanda"}}}}
		}
		if rapid.IntRange(0, 4).Draw(t, name+" joined") == 0 {
			delete(w.id, i)
			p.BrokerID = nil
			continue
		}
		if !rapid.Bool().Draw(t, name+" id known") {
			p.BrokerID = nil
		}
		if rapid.IntRange(0, 4).Draw(t, name+" lost a disk") == 0 {
			w.ghost[i] = 10 + i
		}
	}
	w.unseen = rapid.IntRange(0, 5).Draw(t, "unseen") == 0

	for i := range w.pods {
		if rapid.IntRange(0, 2).Draw(t, fmt.Sprintf("%d answers", i)) == 0 {
			continue
		}
		a := answer{down: []int{}}
		for j := range w.pods {
			id, joined := w.id[j]
			if !joined {
				continue
			}
			if rapid.IntRange(0, 4).Draw(t, fmt.Sprintf("%d lists %d", i, j)) > 0 {
				a.listed = append(a.listed, j)
				if rapid.IntRange(0, 3).Draw(t, fmt.Sprintf("%d respells %d", i, j)) == 0 {
					a.respelled = append(a.respelled, j)
				}
			}
			if j != i && rapid.Bool().Draw(t, fmt.Sprintf("%d reports %d down", i, j)) {
				a.down = append(a.down, id)
			}
		}
		w.answers[i] = a
	}
	return w
}

// decisionChanges are the one change genDecision makes, each a case a rule
// turns on.
var decisionChanges = []string{
	"none", "terminating", "node gone", "up broker's node gone", "unschedulable", "template down", "between crashes",
	"starting", "hung", "candidate hung", "kubelet restarted", "kubelet restarted, alone", "seen alive", "split views",
	"silent", "shared name", "unseen", "lost a disk", "never joined", "never joined, unschedulable", "current never joined",
	"removing", "emptyDir", "hostPath",
	"answered, then crashed", "started since", "candidate split views",
}

// genClusterFor draws a cluster as genCluster does, but half the time the
// decision one of changes makes, for a property about rules only they turn
// on.
func genClusterFor(t *rapid.T, changes ...string) *cluster {
	if rapid.Bool().Draw(t, "its decision") {
		return genDecision(t, rapid.SampledFrom(changes).Draw(t, "change"))
	}
	return genCluster(t)
}

// genDecision draws the case the rule decides on: in pool a, an outdated
// crash-looping candidate and a current broker that is up, with up to two more
// pods, every broker that can answer reporting each as it is; then change, a
// case a rule turns on.
func genDecision(t *rapid.T, change string) *cluster {
	w := &cluster{id: map[int]int{}, ghost: map[int]int{}, answers: map[int]answer{}}
	add := func(name, state, pool string, current bool) {
		p := w.add(name, state)
		p.Pool, p.Current = pool, current
		if !rapid.Bool().Draw(t, fmt.Sprintf("%d id known", len(w.pods)-1)) {
			p.BrokerID = nil
		}
	}
	candidateState := "crash looping"
	switch change {
	case "candidate hung":
		candidateState = "hung"
	case "never joined, unschedulable":
		candidateState = "unschedulable"
	case "kubelet restarted", "kubelet restarted, alone":
		// It runs, but looks hung.
		candidateState = "kubelet restarted"
	case "answered, then crashed":
		candidateState = "running"
		w.then = map[int]string{0: "crash looping"}
	case "answered alone, then kubelet restarted":
		// The only broker that could answer did, then looks hung: only its
		// own word would say it is.
		candidateState = "running"
		w.then = map[int]string{0: "kubelet restarted"}
	}
	add("pod-0", candidateState, "a", false)
	upState := "running"
	switch change {
	case "template down", "kubelet restarted, alone", "answered alone, then kubelet restarted":
		upState = "crash looping"
	case "between crashes", "starting", "hung":
		upState = change
	case "current never joined":
		// Waiting to join, it shows nothing; failing to start, it does.
		upState = rapid.SampledFrom([]string{"hung", "starting", "running", "crash looping", "image pull failing"}).Draw(t, "joiner")
	case "started since":
		upState = "starting"
		w.then = map[int]string{1: "running"}
	}
	add("pod-1", upState, "a", true)
	moreStates := []string{"running", "crash looping"}
	if change == "kubelet restarted, alone" || change == "started since" || change == "answered alone, then kubelet restarted" {
		moreStates = []string{"crash looping"}
	}
	for i := range rapid.IntRange(0, 2).Draw(t, "more pods") {
		add(fmt.Sprintf("pod-%d", len(w.pods)), rapid.SampledFrom(moreStates).Draw(t, fmt.Sprintf("more %d state", i)),
			rapid.SampledFrom([]string{"a", "b"}).Draw(t, fmt.Sprintf("more %d pool", i)), rapid.Bool().Draw(t, fmt.Sprintf("more %d current", i)))
	}
	if change == "started since" {
		// Another that starts as pod-1 does, unasked too.
		add(fmt.Sprintf("pod-%d", len(w.pods)), "starting", "b", true)
		w.then[len(w.pods)-1] = "running"
	}

	switch change {
	case "split views", "candidate split views":
		// Two more that answer, one seeing the up broker down, or the
		// candidate alive.
		add(fmt.Sprintf("pod-%d", len(w.pods)), "running", "b", true)
		add(fmt.Sprintf("pod-%d", len(w.pods)), "running", "b", true)
	case "unschedulable":
		add(fmt.Sprintf("pod-%d", len(w.pods)), "unschedulable", "a", true)
	case "shared name":
		// The up broker's namesake in another cluster.
		add("pod-1", "running", "b", true)
	case "terminating", "node gone":
		i := rapid.IntRange(0, len(w.pods)-1).Draw(t, "terminating pod")
		w.pods[i] = terminating(w.pods[i])
		if change == "node gone" {
			w.goneAt(i)
		}
	case "up broker's node gone":
		w.pods[1] = terminating(w.pods[1])
		w.goneAt(1)
	case "unseen":
		w.unseen = true
	case "lost a disk":
		w.ghost[0] = 10
	case "never joined", "never joined, unschedulable":
		delete(w.id, 0)
		w.pods[0].BrokerID = nil
	case "current never joined":
		delete(w.id, 1)
		w.pods[1].BrokerID = nil
		// One that answers, so membership is known.
		add(fmt.Sprintf("pod-%d", len(w.pods)), "running", "b", true)
	case "removing":
		w.pods[0].Removing = true
	case "emptyDir":
		w.pods[0] = emptyDir(w.pods[0])
	case "hostPath":
		w.pods[0].Spec.Volumes = []corev1.Volume{{Name: "datadir", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/mnt/redpanda"}}}}
	}

	// Its peers, when it answered, were silent or have seen it crash since.
	othersSilent := change == "answered, then crashed" && rapid.Bool().Draw(t, "others silent")
	for i := range w.pods {
		if !w.serves(i) || change == "silent" || othersSilent && i != 0 {
			continue
		}
		a := answer{down: []int{}}
		for j := range w.pods {
			id, joined := w.id[j]
			if !joined {
				continue
			}
			a.listed = append(a.listed, j)
			// A stale status: the candidate's broker is back, its peers see.
			alive := w.alive(j) || change == "seen alive" && j == 0 || change == "candidate split views" && j == 0 && i == len(w.pods)-1
			if change == "split views" && j == 1 && i == len(w.pods)-1 || change == "answered, then crashed" && j == 0 && i != 0 {
				alive = false
			}
			if j != i && !alive {
				a.down = append(a.down, id)
			}
		}
		w.answers[i] = a
	}
	return w
}

// add appends a pod named name in state, its broker joined as the ID its index
// is and known up front, and returns it to adjust before the next add.
func (w *cluster) add(name, state string) *failedpods.Pod {
	i := len(w.pods)
	p := genPod(name, state)
	p.BrokerID = &i
	w.id[i] = i
	w.pods = append(w.pods, p)
	w.state = append(w.state, state)
	w.gone = append(w.gone, false)
	return &w.pods[i]
}

// goneAt marks pod i, terminating, as on a node that's gone.
func (w *cluster) goneAt(i int) {
	w.pods[i] = stuckTerminating(w.pods[i])
	w.gone[i] = true
}

func genPod(name, state string) failedpods.Pod {
	switch state {
	case "running":
		return running(name)
	case "starting":
		return starting(name)
	case "hung", "kubelet restarted":
		return hung(name)
	case "crash looping":
		return crashLooping(name)
	case "between crashes":
		return betweenCrashes(name)
	case "creating":
		return creating(name)
	case "unschedulable":
		return unschedulable(name)
	case "backed off":
		// kubelet's back-off has run out: it may be restarting the container,
		// with its status stale until a PostStart hook returns.
		p := crashLooping(name)
		cs := &p.Status.ContainerStatuses[0]
		cs.State.Waiting.Message = "back-off 10s restarting failed container=redpanda pod=" + name
		cs.LastTerminationState.Terminated.StartedAt = metav1.NewTime(time.Now().Add(-31 * time.Second))
		cs.LastTerminationState.Terminated.FinishedAt = metav1.NewTime(time.Now().Add(-30 * time.Second))
		return p
	default:
		return imagePullFailing(name)
	}
}

// ask asks the brokers, which answer as w says.
func (w *cluster) ask() failedpods.Peers {
	return w.askVouching(-1)
}

// askVouching asks as ask does, but pod vouch's broker answers that every
// broker is up.
func (w *cluster) askVouching(vouch int) failedpods.Peers {
	var all []int
	for i := range w.pods {
		if id, ok := w.id[i]; ok {
			all = append(all, id)
		}
		if ghost, ok := w.ghost[i]; ok {
			all = append(all, ghost)
		}
	}
	return failedpods.AskPeers(context.Background(), w.pods, w.unseen, func(_ context.Context, pod *corev1.Pod) (failedpods.Answer, error) {
		i := w.index(pod)
		a, ok := w.answers[i]
		if !w.serves(i) || !ok && i != vouch {
			return failedpods.Answer{}, errors.New("no answer")
		}
		down := w.downIn(a)
		if i == vouch {
			down = nil
		}
		var brokers []rpadmin.Broker
		for _, j := range a.listed {
			at := func(host string, id int) rpadmin.Broker {
				return rpadmin.Broker{NodeID: id, InternalRPCAddress: host + ":33145"}
			}
			brokers = append(brokers, at(w.pods[j].Name+".rp.ns.svc", w.id[j]))
			if slices.Contains(a.respelled, j) {
				brokers = append(brokers, at(w.pods[j].Name+".rp.ns.svc.cluster.local.", w.id[j]))
			}
			if ghost, ok := w.ghost[j]; ok {
				brokers = append(brokers, at(w.pods[j].Name+".rp.ns.svc", ghost))
			}
		}
		return failedpods.Answer{Health: rpadmin.ClusterHealthOverview{AllNodes: all, NodesDown: down}, Brokers: brokers}, nil
	})
}

// later lists w again, its pods' containers in the states a decision has them
// change to since their peers were asked, or else one redrawn.
func (w *cluster) later(t *rapid.T) *cluster {
	l := *w
	l.askedOf = w
	l.pods, l.state = slices.Clone(w.pods), slices.Clone(w.state)
	then := w.then
	if then == nil {
		then = map[int]string{rapid.IntRange(0, len(w.pods)-1).Draw(t, "changed since"): rapid.SampledFrom(states).Draw(t, "changed to")}
	}
	for i, state := range then {
		p := w.pods[i]
		l.state[i] = state
		fresh := genPod(p.Name, state)
		fresh.Pool, fresh.Current, fresh.BrokerID, fresh.Removing = p.Pool, p.Current, p.BrokerID, p.Removing
		fresh.Spec.Volumes, fresh.DeletionTimestamp = p.Spec.Volumes, p.DeletionTimestamp
		l.pods[i] = fresh
	}
	return &l
}

// asked is the world w's peers were asked in.
func (w *cluster) asked() *cluster {
	if w.askedOf != nil {
		return w.askedOf
	}
	return w
}

func (w *cluster) index(pod *corev1.Pod) int {
	return slices.IndexFunc(w.pods, func(p failedpods.Pod) bool { return p.Pod == pod })
}

// downIn is what a reports down: what it says of the joined brokers, and
// every ghost, which never answers a heartbeat.
func (w *cluster) downIn(a answer) []int {
	down := slices.Clone(a.down)
	for _, ghost := range w.ghost {
		down = append(down, ghost)
	}
	return down
}

// runs reports whether pod i's redpanda container runs.
func (w *cluster) runs(i int) bool {
	return slices.Contains([]string{"running", "starting", "hung", "kubelet restarted", "between crashes"}, w.state[i])
}

// serves reports whether pod i's broker can answer, as the rule can see: it
// runs, has started, and its node is there. One still starting has no admin
// API until it's ready.
func (w *cluster) serves(i int) bool {
	return (w.state[i] == "running" || w.state[i] == "between crashes") && !w.gone[i]
}

// alive reports whether pod i's broker is up, as its peers see: one whose
// kubelet restarted is, though it doesn't look it.
func (w *cluster) alive(i int) bool {
	return (w.serves(i) || w.state[i] == "kubelet restarted") && !w.gone[i]
}

// voice reports whether a pod named name could answer when the peers were
// asked.
func (w *cluster) voice(name string) bool {
	a := w.asked()
	return slices.ContainsFunc(a.indexes(), func(k int) bool { return a.pods[k].Name == name && a.serves(k) })
}

// shared reports whether another pod has pod i's name.
func (w *cluster) shared(i int) bool {
	return slices.ContainsFunc(w.pods, func(p failedpods.Pod) bool { return p.Pod != w.pods[i].Pod && p.Name == w.pods[i].Name })
}

// askable reports whether pod i's broker could answer when its peers were
// asked, and the rule can tell its answer from another's: no other pod shares
// its name.
func (w *cluster) askable(i int) bool {
	a := w.asked()
	return a.serves(i) && !a.shared(i)
}

// answered lists what the brokers besides pod i's answered.
func (w *cluster) answered(i int) []answer {
	a := w.asked()
	var others []answer
	for j := range a.pods {
		if ans, ok := a.answers[j]; ok && j != i && a.askable(j) {
			others = append(others, ans)
		}
	}
	return others
}

// othersMayAnswer reports whether, when its peers were asked, a broker besides
// pod i's could answer, seen or not: one under another name, or under its own
// when another pod shares it, as answers are known by name.
func (w *cluster) othersMayAnswer(i int) bool {
	a := w.asked()
	return a.unseen || slices.ContainsFunc(a.indexes(), func(j int) bool {
		return a.serves(j) && (a.pods[j].Name != a.pods[i].Name || a.shared(i))
	})
}

// brokers is what the rule can know of pod i's broker: its status's ID, or
// else every ID an answer's member list registers at its name, a lost disk's
// old one and a namesake's too.
func (w *cluster) brokers(i int) []int {
	if p := w.pods[i]; p.BrokerID != nil {
		return []int{*p.BrokerID}
	}
	var ids []int
	for j, a := range w.asked().answers {
		if !w.askable(j) {
			continue
		}
		for _, k := range a.listed {
			if w.pods[k].Name != w.pods[i].Name {
				continue
			}
			ids = append(ids, w.id[k])
			if ghost, ok := w.ghost[k]; ok {
				ids = append(ids, ghost)
			}
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

func (w *cluster) seenAlive(i int) bool {
	ids := w.brokers(i)
	return slices.ContainsFunc(w.answered(i), func(a answer) bool {
		return slices.ContainsFunc(ids, func(id int) bool { return !slices.Contains(w.downIn(a), id) })
	})
}

func (w *cluster) recent(i int) bool {
	return w.state[i] == "crash looping" || w.state[i] == "between crashes" || w.state[i] == "backed off"
}

// up: pod i's broker can answer and isn't between crashes, and, with another
// broker that can answer, another answers that it's alive and none that it's
// down.
func (w *cluster) up(i int) bool {
	if !w.serves(i) || w.recent(i) {
		return false
	}
	if !w.othersMayAnswer(i) {
		return true
	}
	ids := w.brokers(i)
	reportedDown := slices.ContainsFunc(w.answered(i), func(a answer) bool {
		return slices.ContainsFunc(ids, func(id int) bool { return slices.Contains(w.downIn(a), id) })
	})
	return w.seenAlive(i) && !reportedDown
}

// anotherDown: another broker answers that one besides pod i's is down, or,
// with no other broker that can answer, pod i's own broker does.
func (w *cluster) anotherDown(i int) bool {
	ids := w.brokers(i)
	another := func(a answer) bool {
		return slices.ContainsFunc(w.downIn(a), func(down int) bool { return !slices.Contains(ids, down) })
	}
	if !w.othersMayAnswer(i) {
		own, ok := w.asked().answers[i]
		return ok && w.askable(i) && another(own)
	}
	return len(ids) > 0 && slices.ContainsFunc(w.answered(i), another)
}

// candidate: a start that hung, as one whose kubelet restarted looks, counts
// only on its peers' word, and, while they answer, one that can't be scheduled
// only if it's a member's.
func (w *cluster) candidate(i int) bool {
	p := w.pods[i]
	failing := w.state[i] == "crash looping" || w.state[i] == "image pull failing" || w.state[i] == "unschedulable" ||
		(w.state[i] == "hung" || w.state[i] == "kubelet restarted") && w.othersMayAnswer(i)
	persistent := p.Spec.Volumes[0].PersistentVolumeClaim != nil || p.Spec.Volumes[0].HostPath != nil
	unheard := w.othersMayAnswer(i) && len(w.answered(i)) == 0
	member := !w.othersMayAnswer(i) || w.state[i] != "unschedulable" || len(w.brokers(i)) > 0
	return !p.Current && !p.Removing && p.DeletionTimestamp == nil && failing && persistent && member && !w.seenAlive(i) && !unheard
}

func (w *cluster) hold(c int) bool {
	if slices.ContainsFunc(w.indexes(), func(j int) bool {
		return w.pods[j].DeletionTimestamp != nil && !w.gone[j] || w.serves(j) && !w.voice(w.pods[j].Name)
	}) {
		return true
	}
	// Whether a broker never joined is known once a member answers; only a
	// member's shows its template runs.
	a := w.asked()
	membersAnswered := slices.ContainsFunc(w.indexes(), func(j int) bool {
		_, answered := a.answers[j]
		return answered && a.askable(j) && len(w.brokers(j)) > 0
	})
	var current []int
	for j, p := range w.pods {
		if p.Pool == w.pods[c].Pool && p.Current && !p.Removing && !w.gone[j] &&
			(w.state[j] == "unschedulable" || !membersAnswered || len(w.brokers(j)) > 0 || w.failsToStart(j)) {
			current = append(current, j)
		}
	}
	return slices.ContainsFunc(current, func(j int) bool { return w.state[j] == "unschedulable" }) ||
		len(current) > 0 && !slices.ContainsFunc(current, func(j int) bool { return len(w.brokers(j)) > 0 && w.up(j) })
}

// failsToStart reports whether pod i's broker can't start, whatever the
// cluster's state.
func (w *cluster) failsToStart(i int) bool {
	return slices.Contains([]string{"crash looping", "between crashes", "backed off", "image pull failing", "unschedulable"}, w.state[i])
}

func (w *cluster) indexes() []int {
	var is []int
	for i := range w.pods {
		is = append(is, i)
	}
	return is
}
